package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/bonyai/tyto-ci/internal/ci"
	"github.com/bonyai/tyto-ci/internal/httpapi"
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
	scheduler, err := ci.NewSchedulerWithStore(profiles, ci.UnconfiguredProvisioner{}, store)
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

func reapExpired(scheduler *ci.Scheduler) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		for _, err := range scheduler.ReapExpired(context.Background(), now) {
			log.Printf("reap expired sandbox: %v", err)
		}
	}
}
