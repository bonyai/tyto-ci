package ci

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProvisioner struct {
	provisioned int
	deleted     int
	err         error
}

func (p *fakeProvisioner) Provision(_ context.Context, request SandboxRequest) (Sandbox, error) {
	p.provisioned++
	if p.err != nil {
		return Sandbox{}, p.err
	}
	return Sandbox{ID: "sbx-test", Profile: request.Profile.Name, ExpiresAt: request.Expires}, nil
}

func (p *fakeProvisioner) Delete(context.Context, Sandbox) error {
	p.deleted++
	return nil
}

func TestSchedulerProvisionsOnceAndCleansUp(t *testing.T) {
	backend := &fakeProvisioner{}
	scheduler, err := NewScheduler([]RunnerProfile{{Name: "tyto", Template: "bonya-dev", Lease: time.Hour, MaxConcurrent: 1}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "github/1", Provider: ProviderGitHub, Project: "bonyai/example", Labels: []string{"tyto"}}

	first, err := scheduler.Provision(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scheduler.Provision(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || backend.provisioned != 1 {
		t.Fatalf("provisioned=%d; expected idempotent provisioning", backend.provisioned)
	}
	if err := scheduler.Cleanup(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if backend.deleted != 1 {
		t.Fatalf("deleted=%d, want 1", backend.deleted)
	}
}

func TestSchedulerReleasesCapacityAfterFailure(t *testing.T) {
	backend := &fakeProvisioner{err: errors.New("unavailable")}
	scheduler, err := NewScheduler([]RunnerProfile{{Name: "tyto", Template: "bonya-dev", Lease: time.Hour, MaxConcurrent: 1}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "github/1", Provider: ProviderGitHub, Project: "bonyai/example", Labels: []string{"tyto"}}
	if _, err := scheduler.Provision(context.Background(), job); err == nil {
		t.Fatal("Provision() error = nil")
	}
	backend.err = nil
	if _, err := scheduler.Provision(context.Background(), job); err != nil {
		t.Fatalf("capacity was not released after error: %v", err)
	}
}
