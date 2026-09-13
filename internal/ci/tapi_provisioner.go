package ci

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

// JITConfigResolver obtains a one-use provider runner configuration. Its
// result is never persisted in JobRecord. The current TAPI job API records
// Env in Temporal history, so deployments must protect or encrypt Temporal
// payloads before enabling this provisioner with real GitHub credentials.
type JITConfigResolver func(context.Context, Job) (string, error)

// TAPIProvisioner uses Compute's durable Temporal job API. TAPI owns sandbox
// creation, the run deadline, and DELETE disposition cleanup; tyto-ci only
// retains the opaque Temporal run ID for cancellation and observability.
type TAPIProvisioner struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	GitHubJIT  JITConfigResolver
}

func (p TAPIProvisioner) Provision(ctx context.Context, request SandboxRequest) (Sandbox, error) {
	if p.BaseURL == "" || p.Token == "" {
		return Sandbox{}, fmt.Errorf("TAPI base URL and token are required")
	}
	if request.Job.Provider != ProviderGitHub {
		return Sandbox{}, fmt.Errorf("TAPI provisioner does not yet support provider %q", request.Job.Provider)
	}
	if p.GitHubJIT == nil {
		return Sandbox{}, fmt.Errorf("GitHub JIT configuration resolver is required")
	}
	jit, err := p.GitHubJIT(ctx, request.Job)
	if err != nil {
		return Sandbox{}, fmt.Errorf("create GitHub JIT configuration: %w", err)
	}
	deadline := int32(time.Until(request.Expires).Seconds())
	if deadline < 1 {
		return Sandbox{}, fmt.Errorf("sandbox lease has already expired")
	}
	body := map[string]any{
		"name":              "tyto-ci " + request.Job.ID,
		"new_sandbox":       map[string]any{"template_id": request.Profile.Template, "name": sandboxName(request.Job.ID)},
		"cmd":               []string{"/bin/sh", "-lc", "exec ./run.sh --jitconfig \"$TYTO_GITHUB_JIT_CONFIG\""},
		"env":               map[string]string{"TYTO_GITHUB_JIT_CONFIG": jit},
		"command_timeout_s": deadline,
		"run_deadline_s":    deadline,
		"max_output_bytes":  65536,
		"disposition":       "delete",
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Sandbox{}, err
	}
	endpoint := strings.TrimSuffix(p.BaseURL, "/") + "/v1/job"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return Sandbox{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.Token)
	httpRequest.Header.Set("Idempotency-Key", "tyto-ci/"+request.Job.ID)
	httpRequest.Header.Set("Content-Type", "application/json")
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return Sandbox{}, fmt.Errorf("start TAPI job: %w", err)
	}
	defer response.Body.Close()
	responseData, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Sandbox{}, err
	}
	if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		return Sandbox{}, fmt.Errorf("start TAPI job: status %d: %s", response.StatusCode, strings.TrimSpace(string(responseData)))
	}
	var result struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(responseData, &result); err != nil {
		return Sandbox{}, fmt.Errorf("decode TAPI job response: %w", err)
	}
	if result.RunID == "" {
		return Sandbox{}, fmt.Errorf("TAPI job response omitted run_id")
	}
	return Sandbox{ID: result.RunID, Profile: request.Profile.Name, ExpiresAt: request.Expires}, nil
}

func (p TAPIProvisioner) Delete(ctx context.Context, sandbox Sandbox) error {
	if p.BaseURL == "" || p.Token == "" {
		return fmt.Errorf("TAPI base URL and token are required")
	}
	if sandbox.ID == "" {
		return fmt.Errorf("Temporal run ID is required")
	}
	endpoint := strings.TrimSuffix(p.BaseURL, "/") + "/v1/job/" + sandbox.ID + "/cancel"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+p.Token)
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("cancel TAPI job: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return fmt.Errorf("cancel TAPI job: status %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
}

func sandboxName(jobID string) string {
	name := strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(jobID)
	return "ci-" + name
}
