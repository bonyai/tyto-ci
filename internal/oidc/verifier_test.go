package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifyAcceptsTrustedSignedToken(t *testing.T) {
	private, document := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(document)
	}))
	defer server.Close()

	verifier, err := New(Config{Issuer: "https://token.actions.githubusercontent.com", JWKSURL: server.URL, Audience: "https://api.tyto.run", ProjectClaims: []string{"repository"}})
	if err != nil {
		t.Fatal(err)
	}
	token := signedToken(t, private, map[string]any{
		"iss":        "https://token.actions.githubusercontent.com",
		"sub":        "repo:bonyai/example:ref:refs/heads/main",
		"aud":        "https://api.tyto.run",
		"repository": "bonyai/example",
		"exp":        time.Now().Add(time.Minute).Unix(),
	})

	identity, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Project != "bonyai/example" {
		t.Fatalf("project=%q", identity.Project)
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	private, document := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(document)
	}))
	defer server.Close()
	verifier, err := New(Config{Issuer: "issuer", JWKSURL: server.URL, Audience: "tyto", ProjectClaims: []string{"repository"}})
	if err != nil {
		t.Fatal(err)
	}
	token := signedToken(t, private, map[string]any{"iss": "issuer", "sub": "subject", "aud": "other", "repository": "bonyai/example", "exp": time.Now().Add(time.Minute).Unix()})
	if _, err := verifier.Verify(context.Background(), token); err == nil {
		t.Fatal("Verify() error = nil")
	}
}

func testKey(t *testing.T) (*rsa.PrivateKey, map[string]any) {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	exponent := big.NewInt(int64(private.PublicKey.E)).Bytes()
	return private, map[string]any{"keys": []map[string]string{{
		"kid": "test-key", "kty": "RSA",
		"n": base64.RawURLEncoding.EncodeToString(private.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(exponent),
	}}}
}

func signedToken(t *testing.T, private *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
