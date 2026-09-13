// Package ci contains provider-neutral job scheduling primitives for Tyto CI.
package ci

import (
	"context"
	"time"
)

type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
)

type Job struct {
	ID        string    `json:"id"`
	Provider  Provider  `json:"provider"`
	Project   string    `json:"project"`
	Ref       string    `json:"ref"`
	CommitSHA string    `json:"commit_sha"`
	Labels    []string  `json:"labels"`
	QueuedAt  time.Time `json:"queued_at"`
}

type RunnerProfile struct {
	Name          string
	Template      string
	Lease         time.Duration
	MaxConcurrent int
}

type SandboxRequest struct {
	Job     Job
	Profile RunnerProfile
	Expires time.Time
}

type Sandbox struct {
	ID        string    `json:"id"`
	Profile   string    `json:"profile"`
	ExpiresAt time.Time `json:"expires_at"`
}

// JobRecord is the durable control-plane record for a provider job. It never
// contains a provider registration token or an OIDC token.
type JobRecord struct {
	Job       Job       `json:"job"`
	Sandbox   Sandbox   `json:"sandbox"`
	CreatedAt time.Time `json:"created_at"`
}

type Provisioner interface {
	Provision(context.Context, SandboxRequest) (Sandbox, error)
	Delete(context.Context, Sandbox) error
}
