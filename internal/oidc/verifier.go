// Package oidc verifies CI-provider job identity tokens against configured
// issuers and JWKS endpoints. It deliberately accepts only RS256, the signing
// algorithm used by GitHub Actions and GitLab CI job ID tokens.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Issuer        string
	JWKSURL       string
	Audience      string
	ProjectClaims []string
}

type Identity struct {
	Subject   string
	Project   string
	ExpiresAt time.Time
	Claims    map[string]any
}

type Verifier struct {
	config Config
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	refresh time.Time
}

func New(config Config) (*Verifier, error) {
	if config.Issuer == "" || config.JWKSURL == "" || config.Audience == "" || len(config.ProjectClaims) == 0 {
		return nil, errors.New("issuer, JWKS URL, audience, and project claims are required")
	}
	return &Verifier{config: config, client: &http.Client{Timeout: 5 * time.Second}, now: time.Now}, nil
}

func (v *Verifier) Verify(ctx context.Context, token string) (Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, errors.New("OIDC token must have three parts")
	}
	header, err := decodeJSON(parts[0])
	if err != nil {
		return Identity{}, fmt.Errorf("decode OIDC header: %w", err)
	}
	if header["alg"] != "RS256" {
		return Identity{}, errors.New("OIDC token must use RS256")
	}
	kid, _ := header["kid"].(string)
	if kid == "" {
		return Identity{}, errors.New("OIDC token is missing key id")
	}
	claims, err := decodeJSON(parts[1])
	if err != nil {
		return Identity{}, fmt.Errorf("decode OIDC claims: %w", err)
	}
	if err := v.validateClaims(claims); err != nil {
		return Identity{}, err
	}
	key, err := v.key(ctx, kid)
	if err != nil {
		return Identity{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, fmt.Errorf("decode OIDC signature: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Identity{}, errors.New("OIDC signature is invalid")
	}

	subject, _ := claims["sub"].(string)
	project := firstString(claims, v.config.ProjectClaims)
	return Identity{Subject: subject, Project: project, ExpiresAt: unixClaim(claims["exp"]), Claims: claims}, nil
}

func (v *Verifier) validateClaims(claims map[string]any) error {
	issuer, _ := claims["iss"].(string)
	if issuer != v.config.Issuer {
		return errors.New("OIDC issuer is not trusted")
	}
	if subject, _ := claims["sub"].(string); subject == "" {
		return errors.New("OIDC subject is required")
	}
	if !containsAudience(claims["aud"], v.config.Audience) {
		return errors.New("OIDC audience is not accepted")
	}
	now := v.now()
	expires := unixClaim(claims["exp"])
	if expires.IsZero() || !expires.After(now) {
		return errors.New("OIDC token is expired")
	}
	if notBefore := unixClaim(claims["nbf"]); !notBefore.IsZero() && notBefore.After(now.Add(30*time.Second)) {
		return errors.New("OIDC token is not valid yet")
	}
	if firstString(claims, v.config.ProjectClaims) == "" {
		return errors.New("OIDC token is missing project identity")
	}
	return nil
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	key := v.keys[kid]
	fresh := v.now().Before(v.refresh)
	v.mu.Unlock()
	if key != nil && fresh {
		return key, nil
	}
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}
	v.mu.Lock()
	key = v.keys[kid]
	v.mu.Unlock()
	if key == nil {
		return nil, fmt.Errorf("OIDC signing key %q is unavailable", kid)
	}
	return key, nil
}

func (v *Verifier) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JWKSURL, nil)
	if err != nil {
		return err
	}
	response, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch OIDC JWKS: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch OIDC JWKS: unexpected status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	var document struct {
		Keys []struct {
			KID string `json:"kid"`
			KTY string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode OIDC JWKS: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(document.Keys))
	for _, item := range document.Keys {
		if item.KTY != "RSA" || item.KID == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(item.N)
		if err != nil {
			continue
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(item.E)
		if err != nil || len(exponentBytes) == 0 {
			continue
		}
		exponent := 0
		for _, b := range exponentBytes {
			exponent = exponent<<8 | int(b)
		}
		if exponent < 3 || len(modulus) < 256 {
			continue
		}
		keys[item.KID] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}
	}
	if len(keys) == 0 {
		return errors.New("OIDC JWKS contains no usable RSA keys")
	}
	v.mu.Lock()
	v.keys = keys
	v.refresh = v.now().Add(5 * time.Minute)
	v.mu.Unlock()
	return nil
}

func decodeJSON(part string) (map[string]any, error) {
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func containsAudience(value any, expected string) bool {
	switch audience := value.(type) {
	case string:
		return audience == expected
	case []any:
		for _, item := range audience {
			if text, _ := item.(string); text == expected {
				return true
			}
		}
	}
	return false
}

func firstString(claims map[string]any, names []string) string {
	for _, name := range names {
		if value, _ := claims[name].(string); value != "" {
			return value
		}
	}
	return ""
}

func unixClaim(value any) time.Time {
	number, ok := value.(json.Number)
	if !ok {
		return time.Time{}
	}
	seconds, err := number.Int64()
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}
