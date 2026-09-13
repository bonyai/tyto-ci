package ci

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Scheduler maps a provider job's Tyto label to an administrator-controlled
// profile, enforces its local concurrency limit, and asks a Provisioner to
// create the leased sandbox. Provider adapters never select raw templates.
type Scheduler struct {
	mu       sync.Mutex
	profiles map[string]RunnerProfile
	active   map[string]int
	jobs     map[string]Sandbox
	jobInfo  map[string]Job
	inflight map[string]chan struct{}
	backend  Provisioner
	store    Store
}

func NewScheduler(profiles []RunnerProfile, backend Provisioner) (*Scheduler, error) {
	return NewSchedulerWithStore(profiles, backend, NewMemoryStore())
}

func NewSchedulerWithStore(profiles []RunnerProfile, backend Provisioner, store Store) (*Scheduler, error) {
	if backend == nil {
		return nil, fmt.Errorf("provisioner is required")
	}
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	s := &Scheduler{profiles: make(map[string]RunnerProfile), active: make(map[string]int), jobs: make(map[string]Sandbox), jobInfo: make(map[string]Job), inflight: make(map[string]chan struct{}), backend: backend, store: store}
	for _, profile := range profiles {
		if profile.Name == "" || profile.Template == "" || profile.Lease <= 0 || profile.MaxConcurrent < 1 {
			return nil, fmt.Errorf("invalid runner profile %q", profile.Name)
		}
		if _, exists := s.profiles[profile.Name]; exists {
			return nil, fmt.Errorf("duplicate runner profile %q", profile.Name)
		}
		s.profiles[profile.Name] = profile
	}
	records, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load job store: %w", err)
	}
	for _, record := range records {
		if record.Sandbox.ExpiresAt.After(time.Now()) {
			s.jobs[record.Job.ID], s.jobInfo[record.Job.ID] = record.Sandbox, record.Job
			s.active[record.Sandbox.Profile]++
		}
	}
	return s, nil
}

func (s *Scheduler) Provision(ctx context.Context, job Job) (Sandbox, error) {
	if job.ID == "" || job.Provider == "" || job.Project == "" {
		return Sandbox{}, fmt.Errorf("job id, provider, and project are required")
	}
	profile, err := s.profileFor(job.Labels)
	if err != nil {
		return Sandbox{}, err
	}

	s.mu.Lock()
	if sandbox, exists := s.jobs[job.ID]; exists {
		s.mu.Unlock()
		return sandbox, nil
	}
	if wait, exists := s.inflight[job.ID]; exists {
		s.mu.Unlock()
		select {
		case <-wait:
			return s.Provision(ctx, job)
		case <-ctx.Done():
			return Sandbox{}, ctx.Err()
		}
	}
	if s.active[profile.Name] >= profile.MaxConcurrent {
		s.mu.Unlock()
		return Sandbox{}, fmt.Errorf("runner profile %q is at capacity", profile.Name)
	}
	s.active[profile.Name]++
	wait := make(chan struct{})
	s.inflight[job.ID] = wait
	s.mu.Unlock()

	sandbox, err := s.backend.Provision(ctx, SandboxRequest{Job: job, Profile: profile, Expires: time.Now().Add(profile.Lease)})
	if err != nil {
		s.mu.Lock()
		s.active[profile.Name]--
		delete(s.inflight, job.ID)
		close(wait)
		s.mu.Unlock()
		return Sandbox{}, err
	}
	if sandbox.Profile == "" {
		sandbox.Profile = profile.Name
	}

	s.mu.Lock()
	s.jobs[job.ID] = sandbox
	s.jobInfo[job.ID] = job
	s.mu.Unlock()
	if err := s.store.Put(JobRecord{Job: job, Sandbox: sandbox, CreatedAt: time.Now().UTC()}); err != nil {
		// The sandbox exists but cannot be safely recovered after a restart.
		_ = s.backend.Delete(ctx, sandbox)
		s.mu.Lock()
		delete(s.jobs, job.ID)
		delete(s.jobInfo, job.ID)
		delete(s.inflight, job.ID)
		close(wait)
		s.active[profile.Name]--
		s.mu.Unlock()
		return Sandbox{}, fmt.Errorf("persist job record: %w", err)
	}
	s.mu.Lock()
	delete(s.inflight, job.ID)
	close(wait)
	s.mu.Unlock()
	return sandbox, nil
}

func (s *Scheduler) Cleanup(ctx context.Context, jobID string) error {
	s.mu.Lock()
	sandbox, exists := s.jobs[jobID]
	if exists {
		delete(s.jobs, jobID)
		delete(s.jobInfo, jobID)
		s.active[sandbox.Profile]--
	}
	s.mu.Unlock()
	if !exists {
		return nil
	}
	if err := s.backend.Delete(ctx, sandbox); err != nil {
		return err
	}
	return s.store.Delete(jobID)
}

// ReapExpired deletes all sandbox leases that have expired. A production Tyto
// provisioner must independently enforce the same expiry server-side.
func (s *Scheduler) ReapExpired(ctx context.Context, now time.Time) []error {
	s.mu.Lock()
	ids := make([]string, 0)
	for id, sandbox := range s.jobs {
		if !sandbox.ExpiresAt.After(now) {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := s.Cleanup(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (s *Scheduler) Active() []JobRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]JobRecord, 0, len(s.jobs))
	for id, sandbox := range s.jobs {
		records = append(records, JobRecord{Job: s.jobInfo[id], Sandbox: sandbox})
	}
	return records
}

func (s *Scheduler) profileFor(labels []string) (RunnerProfile, error) {
	for _, label := range labels {
		if profile, ok := s.profiles[strings.ToLower(label)]; ok {
			return profile, nil
		}
	}
	return RunnerProfile{}, fmt.Errorf("no Tyto runner profile matches labels %q", labels)
}
