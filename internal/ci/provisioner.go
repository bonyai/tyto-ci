package ci

import (
	"context"
	"errors"
)

// ErrProvisioningUnavailable makes the current integration boundary explicit:
// Tyto's public sandbox API must first grow CI leases and trusted job metadata.
var ErrProvisioningUnavailable = errors.New("sandbox provisioning is not configured")

type UnconfiguredProvisioner struct{}

func (UnconfiguredProvisioner) Provision(context.Context, SandboxRequest) (Sandbox, error) {
	return Sandbox{}, ErrProvisioningUnavailable
}

func (UnconfiguredProvisioner) Delete(context.Context, Sandbox) error { return nil }
