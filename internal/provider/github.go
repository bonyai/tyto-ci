// Package provider contains narrow, testable provider API clients. Provider
// credentials are inputs to calls and are never persisted by tyto-ci.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GitHubJITClient struct {
	BaseURL string
	Client  *http.Client
}

type JITRunner struct {
	Name    string
	Encoded string
}

// CreateJITRunner creates a single-use GitHub Actions registration
// configuration for an installation token. The caller is responsible for
// minting the short-lived GitHub App installation token.
func (c GitHubJITClient) CreateJITRunner(ctx context.Context, installationToken, owner, repo, name string, labels []string) (JITRunner, error) {
	if installationToken == "" || owner == "" || repo == "" || name == "" {
		return JITRunner{}, fmt.Errorf("installation token, owner, repo, and runner name are required")
	}
	base := strings.TrimSuffix(c.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	body, _ := json.Marshal(map[string]any{"name": name, "runner_group_id": 1, "labels": labels})
	url := base + "/repos/" + owner + "/" + repo + "/actions/runners/generate-jitconfig"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return JITRunner{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+installationToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return JITRunner{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return JITRunner{}, err
	}
	if response.StatusCode != http.StatusCreated {
		return JITRunner{}, fmt.Errorf("GitHub JIT runner: status %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		EncodedJITConfig string `json:"encoded_jit_config"`
		Runner           struct {
			Name string `json:"name"`
		} `json:"runner"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return JITRunner{}, err
	}
	if result.EncodedJITConfig == "" {
		return JITRunner{}, fmt.Errorf("GitHub JIT runner response omitted configuration")
	}
	return JITRunner{Name: result.Runner.Name, Encoded: result.EncodedJITConfig}, nil
}
