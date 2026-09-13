package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bonyai/tyto-ci/internal/ci"
	"github.com/bonyai/tyto-ci/internal/httpapi"
	"github.com/bonyai/tyto-ci/internal/provider"
)

func main() {
	profiles := []ci.RunnerProfile{
		{Name: "tyto", Template: "bonya-dev", Lease: 2 * time.Hour, MaxConcurrent: 10},
		{Name: "tyto-large", Template: "bonya-dev", Lease: 2 * time.Hour, MaxConcurrent: 2},
		{Name: "tyto-gpu", Template: "bonya-dev-gpu", Lease: 2 * time.Hour, MaxConcurrent: 1},
	}

	stateFile := os.Getenv("TYTO_CI_STATE_FILE")
	if stateFile == "" {
		stateFile = "./tyto-ci-state.json"
	}
	store, err := ci.NewFileStore(stateFile)
	if err != nil {
		log.Fatal(err)
	}
	backend, err := provisionerFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	scheduler, err := ci.NewSchedulerWithStore(profiles, backend, store)
	if err != nil {
		log.Fatal(err)
	}

	server := httpapi.New(httpapi.Config{
		GitHubWebhookSecret: os.Getenv("TYTO_CI_GITHUB_WEBHOOK_SECRET"),
		GitLabWebhookToken:  os.Getenv("TYTO_CI_GITLAB_WEBHOOK_TOKEN"),
	}, scheduler)

	address := os.Getenv("TYTO_CI_LISTEN_ADDR")
	if address == "" {
		address = ":8080"
	}

	httpServer := &http.Server{Addr: address, Handler: server, ReadHeaderTimeout: 5 * time.Second}
	go reapExpired(scheduler)
	log.Printf("tyto-ci listening on %s", address)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func provisionerFromEnvironment() (ci.Provisioner, error) {
	baseURL, token := os.Getenv("TYTO_CI_TAPI_URL"), os.Getenv("TYTO_CI_TAPI_TOKEN")
	if baseURL == "" && token == "" {
		return ci.UnconfiguredProvisioner{}, nil
	}
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("TYTO_CI_TAPI_URL and TYTO_CI_TAPI_TOKEN must be set together")
	}
	installationToken := os.Getenv("TYTO_CI_GITHUB_INSTALLATION_TOKEN")
	if installationToken == "" {
		return nil, fmt.Errorf("TYTO_CI_GITHUB_INSTALLATION_TOKEN is required with TAPI provisioning")
	}
	github := provider.GitHubJITClient{BaseURL: os.Getenv("TYTO_CI_GITHUB_API_URL")}
	return ci.TAPIProvisioner{BaseURL: baseURL, Token: token, GitHubJIT: func(ctx context.Context, job ci.Job) (string, error) {
		owner, repo, found := strings.Cut(job.Project, "/")
		if !found || owner == "" || repo == "" {
			return "", fmt.Errorf("GitHub project must be owner/repository, got %q", job.Project)
		}
		name := "tyto-" + strings.NewReplacer("/", "-", "_", "-").Replace(job.ID)
		jit, err := github.CreateJITRunner(ctx, installationToken, owner, repo, name, job.Labels)
		if err != nil {
			return "", err
		}
		return jit.Encoded, nil
	}}, nil
}

func reapExpired(scheduler *ci.Scheduler) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		for _, err := range scheduler.ReapExpired(context.Background(), now) {
			log.Printf("reap expired sandbox: %v", err)
		}
	}
}
