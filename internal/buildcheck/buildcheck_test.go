package buildcheck

import (
	"strings"
	"testing"
)

func TestBootstrapCannotSurviveActivation(t *testing.T) {
	t.Parallel()
	if err := CheckKernelPin("hctl@bootstrap", "bootstrap"); err != nil {
		t.Fatal(err)
	}
	if err := CheckKernelPin("hctl@bootstrap", "active"); err == nil || !strings.Contains(err.Error(), "STALE_BINARY") {
		t.Fatalf("active bootstrap pin should fail closed, got %v", err)
	}
}

func TestMismatchedOIDFailsClosed(t *testing.T) {
	t.Parallel()
	if err := CheckKernelPin(strings.Repeat("a", 40), "active"); err == nil || !strings.Contains(err.Error(), "STALE_BINARY") {
		t.Fatalf("mismatched build revision should fail closed, got %v", err)
	}
}

func TestDevelopmentBuildCannotClaimTaggedSemver(t *testing.T) {
	t.Parallel()
	if Current().ModuleVersion == "v"+Version {
		t.Skip("test binary was built as the matching tagged module")
	}
	if err := CheckKernelPin("hctl@v"+Version, "active"); err == nil || !strings.Contains(err.Error(), "STALE_BINARY") {
		t.Fatalf("development build should not satisfy semver pin, got %v", err)
	}
}
