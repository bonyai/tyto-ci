package ci

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTAPIProvisionerStartsDisposableJobAndCancelsIt(t *testing.T) {
	var created map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/job" {
			if r.Header.Get("Authorization") != "Bearer tapi-token" {
				t.Fatal("missing TAPI credential")
			}
			if r.Header.Get("Idempotency-Key") != "tyto-ci/github/123" {
				t.Fatal("wrong idempotency key")
			}
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"run_id":"run-123"}`))
			return
		}
		if r.URL.Path == "/v1/job/run-123/cancel" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected request: %s", r.URL.Path)
	}))
	defer server.Close()
	p := TAPIProvisioner{BaseURL: server.URL, Token: "tapi-token", GitHubJIT: func(context.Context, Job) (string, error) { return "jit-secret", nil }}
	sandbox, err := p.Provision(context.Background(), SandboxRequest{Job: Job{ID: "github/123", Provider: ProviderGitHub}, Profile: RunnerProfile{Name: "tyto", Template: "bonya-dev"}, Expires: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.ID != "run-123" {
		t.Fatalf("sandbox ID = %q", sandbox.ID)
	}
	if err := p.Delete(context.Background(), sandbox); err != nil {
		t.Fatal(err)
	}
	if created["disposition"] != "delete" {
		t.Fatalf("disposition=%v", created["disposition"])
	}
	env := created["env"].(map[string]any)
	if env["TYTO_GITHUB_JIT_CONFIG"] != "jit-secret" {
		t.Fatal("JIT config not supplied to job")
	}
}
