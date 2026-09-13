package policy

import "testing"

func TestRejectsUnsupportedJobs(t *testing.T) {
	if err := Validate(JobSpec{UsesContainer: true}); err == nil {
		t.Fatal("container job accepted")
	}
	if err := Validate(JobSpec{}); err != nil {
		t.Fatal(err)
	}
}
