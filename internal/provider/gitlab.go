package provider

import "fmt"

// GitLabExecutorConfig is injected only into the ephemeral sandbox process.
// GitLab custom executors receive job data from the runner manager; no GitLab
// registration token is recorded in the Tyto CI store.
type GitLabExecutorConfig struct {
	URL         string
	RunnerToken string
	RunnerName  string
	Tags        []string
}

func (c GitLabExecutorConfig) Validate() error {
	if c.URL == "" || c.RunnerToken == "" || c.RunnerName == "" {
		return fmt.Errorf("GitLab URL, runner token, and runner name are required")
	}
	return nil
}
