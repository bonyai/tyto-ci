package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bonyai/tyto-ci/internal/ci"
)

type Config struct {
	GitHubWebhookSecret string
	GitLabWebhookToken  string
}

func New(config Config, scheduler *ci.Scheduler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if scheduler == nil || config.GitHubWebhookSecret == "" || config.GitLabWebhookToken == "" {
			http.Error(w, "CI provider configuration is incomplete", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("tyto_ci_active_jobs " + strconv.Itoa(len(scheduler.Active())) + "\n"))
	})
	mux.HandleFunc("POST /webhooks/github", githubWebhook(config.GitHubWebhookSecret, scheduler))
	mux.HandleFunc("POST /webhooks/gitlab", gitlabWebhook(config.GitLabWebhookToken, scheduler))
	return mux
}

func githubWebhook(secret string, scheduler *ci.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil || !validGitHubSignature(secret, r.Header.Get("X-Hub-Signature-256"), body) {
			http.Error(w, "invalid GitHub webhook", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-GitHub-Event") != "workflow_job" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var event struct {
			Action     string `json:"action"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			WorkflowJob struct {
				ID         int64    `json:"id"`
				Labels     []string `json:"labels"`
				HeadSHA    string   `json:"head_sha"`
				HeadBranch string   `json:"head_branch"`
				Conclusion string   `json:"conclusion"`
			} `json:"workflow_job"`
		}
		if json.Unmarshal(body, &event) != nil {
			http.Error(w, "invalid GitHub webhook", http.StatusBadRequest)
			return
		}
		jobID := "github/" + itoa(event.WorkflowJob.ID)
		if event.Action == "completed" && event.WorkflowJob.Conclusion == "cancelled" {
			respondProvisionError(w, scheduler.Cleanup(r.Context(), jobID))
			return
		}
		if event.Action != "queued" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, err = scheduler.Provision(r.Context(), ci.Job{ID: jobID, Provider: ci.ProviderGitHub, Project: event.Repository.FullName, Ref: event.WorkflowJob.HeadBranch, CommitSHA: event.WorkflowJob.HeadSHA, Labels: event.WorkflowJob.Labels, QueuedAt: time.Now().UTC()})
		respondProvisionError(w, err)
	}
}

func gitlabWebhook(token string, scheduler *ci.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" || !hmac.Equal([]byte(token), []byte(r.Header.Get("X-Gitlab-Token"))) {
			http.Error(w, "invalid GitLab webhook", http.StatusUnauthorized)
			return
		}
		var event struct {
			ObjectKind string `json:"object_kind"`
			Project    struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
			Object struct {
				ID      int64    `json:"id"`
				Status  string   `json:"status"`
				Ref     string   `json:"ref"`
				SHA     string   `json:"sha"`
				TagList []string `json:"tag_list"`
			} `json:"object_attributes"`
		}
		if json.NewDecoder(r.Body).Decode(&event) != nil {
			http.Error(w, "invalid GitLab webhook", http.StatusBadRequest)
			return
		}
		jobID := "gitlab/" + itoa(event.Object.ID)
		if event.ObjectKind == "build" && event.Object.Status == "canceled" {
			respondProvisionError(w, scheduler.Cleanup(r.Context(), jobID))
			return
		}
		if event.ObjectKind != "build" || event.Object.Status != "pending" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, err := scheduler.Provision(r.Context(), ci.Job{ID: jobID, Provider: ci.ProviderGitLab, Project: event.Project.PathWithNamespace, Ref: event.Object.Ref, CommitSHA: event.Object.SHA, Labels: event.Object.TagList, QueuedAt: time.Now().UTC()})
		respondProvisionError(w, err)
	}
}

func readBody(r *http.Request) ([]byte, error) {
	const maxWebhookBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxWebhookBytes {
		return nil, errors.New("webhook body exceeds 1 MiB")
	}
	return body, nil
}

func validGitHubSignature(secret, signature string, body []byte) bool {
	if secret == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func respondProvisionError(w http.ResponseWriter, err error) {
	if err == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if errors.Is(err, ci.ErrProvisioningUnavailable) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusUnprocessableEntity)
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }
