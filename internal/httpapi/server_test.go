package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bonyai/tyto-ci/internal/ci"
)

func TestGitHubWebhookRejectsBadSignature(t *testing.T) {
	scheduler, err := ci.NewScheduler([]ci.RunnerProfile{{Name: "tyto", Template: "bonya-dev", Lease: time.Hour, MaxConcurrent: 1}}, ci.UnconfiguredProvisioner{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewBufferString(`{"action":"queued"}`))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	response := httptest.NewRecorder()
	New(Config{GitHubWebhookSecret: "secret"}, scheduler).ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestGitHubWebhookAcceptsAuthenticatedQueuedTytoJob(t *testing.T) {
	secret := "secret"
	body := []byte(`{"action":"queued","repository":{"full_name":"bonyai/example"},"workflow_job":{"id":42,"labels":["tyto"],"head_sha":"abc","head_branch":"main"}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	scheduler, err := ci.NewScheduler([]ci.RunnerProfile{{Name: "tyto", Template: "bonya-dev", Lease: time.Hour, MaxConcurrent: 1}}, ci.UnconfiguredProvisioner{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	New(Config{GitHubWebhookSecret: secret}, scheduler).ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
