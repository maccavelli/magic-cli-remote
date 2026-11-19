package registered_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/registered"
)

// strangerSection is a section no provider owns.
type strangerSection struct{}

func (*strangerSection) IsEnabled() bool { return true }
func (*strangerSection) Validate() error { return nil }

// panicText runs f and returns the text of its panic, "" if it did not panic.
func panicText(f func()) (text string) {
	defer func() {
		if r := recover(); r != nil {
			text = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

// A provider built from another provider's section is a wiring mistake and
// panics, with the text the runtime's own type assertion failure has: both types
// named. It was a bare assertion; the two-value form is what errcheck's
// check-type-assertions wants, and this holds the message it kept.
func TestProvidersPanicNamingBothTypesWhenBuiltFromAnotherSection(t *testing.T) {
	for _, d := range registered.Descriptors() {
		if d.ID == provider.IDFake {
			continue // the fake provider reads no section of its own
		}
		t.Run(string(d.ID), func(t *testing.T) {
			got := panicText(func() { d.New(provider.Build{Section: &strangerSection{}}) })
			if !strings.HasPrefix(got, "interface conversion: config.ProviderSection is *registered_test.strangerSection, not *") ||
				!strings.HasSuffix(got, ".Section") {
				t.Fatalf("New with another provider's section panicked with %q, want the runtime's text naming both types", got)
			}
		})
	}
}

func TestCodexCredentialsPanicNamingBothTypesForAnotherSection(t *testing.T) {
	for _, d := range registered.Descriptors() {
		if d.ID != provider.IDCodex {
			continue
		}
		got := panicText(func() { d.Credentials(&strangerSection{}) })
		if !strings.HasPrefix(got, "interface conversion: config.ProviderSection is *registered_test.strangerSection, not *codex.Section") {
			t.Fatalf("Credentials with another section panicked with %q, want the runtime's text naming both types", got)
		}
	}
}
