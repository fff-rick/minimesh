package buildinfo

import "testing"

func TestVersionIsDefined(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
