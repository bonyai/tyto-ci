package ci

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreSurvivesSchedulerRestart(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := RunnerProfile{Name: "tyto", Template: "bonya-dev", Lease: time.Hour, MaxConcurrent: 1}
	backend := &fakeProvisioner{}
	first, err := NewSchedulerWithStore([]RunnerProfile{profile}, backend, store)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "github/1", Provider: ProviderGitHub, Project: "bonyai/example", Labels: []string{"tyto"}}
	if _, err := first.Provision(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	second, err := NewSchedulerWithStore([]RunnerProfile{profile}, backend, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Provision(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if backend.provisioned != 1 {
		t.Fatalf("provisioned=%d, want 1", backend.provisioned)
	}
}

func TestSchedulerReapsExpiredLease(t *testing.T) {
	backend := &fakeProvisioner{}
	scheduler, err := NewScheduler([]RunnerProfile{{Name: "tyto", Template: "bonya-dev", Lease: time.Hour, MaxConcurrent: 1}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "github/1", Provider: ProviderGitHub, Project: "bonyai/example", Labels: []string{"tyto"}}
	if _, err := scheduler.Provision(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	scheduler.mu.Lock()
	sandbox := scheduler.jobs[job.ID]
	sandbox.ExpiresAt = time.Now().Add(-time.Second)
	scheduler.jobs[job.ID] = sandbox
	scheduler.mu.Unlock()
	if errs := scheduler.ReapExpired(context.Background(), time.Now()); len(errs) != 0 {
		t.Fatalf("reap errors: %v", errs)
	}
	if backend.deleted != 1 {
		t.Fatalf("deleted=%d, want 1", backend.deleted)
	}
}
