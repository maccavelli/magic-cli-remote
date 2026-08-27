package cli

import (
	"os"
	"runtime"
	"testing"
)

// TestShutdownSignals pins MADR 0116 D9: os.Interrupt everywhere, and SIGTERM
// only where the platform actually delivers it.
func TestShutdownSignals(t *testing.T) {
	got := shutdownSignals()
	if len(got) == 0 {
		t.Fatal("shutdownSignals() is empty")
	}
	found := false
	for _, s := range got {
		if s == os.Interrupt {
			found = true
		}
	}
	if !found {
		t.Errorf("shutdownSignals() = %v, want it to include os.Interrupt", got)
	}
	if runtime.GOOS == "windows" && len(got) != 1 {
		t.Errorf("shutdownSignals() = %v; Windows never delivers SIGTERM, so os.Interrupt must stand alone", got)
	}
	if runtime.GOOS != "windows" && len(got) != 2 {
		t.Errorf("shutdownSignals() = %v, want os.Interrupt and SIGTERM", got)
	}
}
