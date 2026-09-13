// Package policy makes the v1 compatibility boundary explicit before a
// sandbox is provisioned.
package policy

import "fmt"

type JobSpec struct {
	UsesContainer bool
	UsesServices  bool
	UsesDocker    bool
}

func Validate(spec JobSpec) error {
	if spec.UsesContainer {
		return fmt.Errorf("container jobs are not supported by Tyto managed runners")
	}
	if spec.UsesServices {
		return fmt.Errorf("service containers are not supported by Tyto managed runners")
	}
	if spec.UsesDocker {
		return fmt.Errorf("Docker builds are not supported by Tyto managed runners")
	}
	return nil
}
