// Package bootstrap builds safe, non-secret-bearing runner startup commands.
package bootstrap

import (
	"fmt"
	"strings"
)

// GitHubCommand starts the official runner with a JIT configuration. The JIT
// payload is intentionally supplied separately as an environment value so it
// is not visible in process argument listings.
func GitHubCommand(workDir string) ([]string, error) {
	if strings.TrimSpace(workDir) == "" {
		return nil, fmt.Errorf("work directory is required")
	}
	return []string{"./run.sh", "--jitconfig", "$TYTO_GITHUB_JIT_CONFIG"}, nil
}

// GitLabCustomExecutorCommand is the runner-manager invocation used by the
// GitLab custom executor image. Jobs that declare image/services are rejected
// by policy before this command is reached.
func GitLabCustomExecutorCommand(configPath string) ([]string, error) {
	if strings.TrimSpace(configPath) == "" {
		return nil, fmt.Errorf("GitLab runner config path is required")
	}
	return []string{"gitlab-runner", "run", "--config", configPath}, nil
}
