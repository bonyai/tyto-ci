package ci

import (
	"context"
	"errors"
)

// ErrProvisioningUnavailable is returned when the deployment has not enabled
// the TAPI Temporal-job provisioner.
var ErrProvisioningUnavailable = errors.New("sandbox provisioning is not configured")

type UnconfiguredProvisioner struct{}

func (UnconfiguredProvisioner) Provision(context.Context, SandboxRequest) (Sandbox, error) {
	return Sandbox{}, ErrProvisioningUnavailable
}

func (UnconfiguredProvisioner) Delete(context.Context, Sandbox) error { return nil }
