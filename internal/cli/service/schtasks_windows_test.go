//go:build windows

package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

// The HRESULTs Task Scheduler returns for a definition it rejects. The
// library prints a COM failure as "HRESULT 0x%08X", which no UI language
// changes.
const (
	hrSchedInvalidValue = "0x80041318" // SCHED_E_INVALIDVALUE: parsed, a value is wrong
	hrSchedMalformedXML = "0x8004131A" // SCHED_E_MALFORMEDXML: not parsed
)

// TestRenderedTaskParsesInRealTaskScheduler is PLAN 0116 rows 15-17 (MADR
// 0116 D24), moved onto go-selfupdate-lib's Register (MADR 0207 D17).
//
// Every other service test fakes Task Scheduler, which is how a definition it
// cannot parse once shipped (F25). This one hands the definition setup
// registers to the real Task Scheduler, through the library, as setup does.
//
// It must never register a task (PLAN C8). The definition carries one invalid
// value, <ExecutionTimeLimit>NOT_A_DURATION, so a definition that PARSES is
// still rejected at validation, while one that does not parse is rejected as
// malformed. The name is throwaway, it is deleted in cleanup whatever happens,
// and the test asserts it does not exist afterwards. A rejection it cannot
// classify is a skip that says so, never a pass.
func TestRenderedTaskParsesInRealTaskScheduler(t *testing.T) {
	body, err := renderTaskXML(windowsOpts(), currentTaskUser())
	if err != nil {
		t.Fatal(err)
	}
	const valid, invalid = "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "<ExecutionTimeLimit>NOT_A_DURATION</ExecutionTimeLimit>"
	if strings.Count(body, valid) != 1 {
		t.Fatalf("rendered definition does not contain %s exactly once; the no-registration technique cannot be applied", valid)
	}
	poisoned := strings.Replace(body, valid, invalid, 1)

	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "mc-0207-u4-probe-" + hex.EncodeToString(suffix[:])
	task, err := openTask(name)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() {
		_ = task.Delete(ctx, name)
		if st, err := task.Status(ctx, name); err != nil || st.Installed {
			t.Errorf("PLAN C8 breached: task %q is registered after cleanup (%+v, %v)", name, st, err)
		}
	})

	t.Run("the rendered definition parses", func(t *testing.T) {
		err := task.Register(ctx, name, poisoned)
		switch {
		case err == nil:
			t.Fatalf("Task Scheduler registered a definition with an invalid value; C8 relies on that being impossible")
		case strings.Contains(err.Error(), hrSchedMalformedXML):
			t.Fatalf("Task Scheduler could not parse the rendered definition: %v", err)
		case strings.Contains(err.Error(), hrSchedInvalidValue):
			// Parsed, then rejected only the injected value.
		default:
			t.Skipf("unclassified rejection; cannot tell parse from validation: %v", err)
		}
	})

	// The control must be text Go parses and Task Scheduler does not: the library
	// parses a definition before it reaches Task Scheduler, so a truncated one is
	// refused in Go and never yields a result to classify (0207 PLAN U4
	// Deviation 3). Go's decoder accepts a repeated attribute; Task Scheduler's
	// parser answers it with SCHED_E_MALFORMEDXML (probed on a real host).
	t.Run("negative control: a definition only Task Scheduler cannot parse", func(t *testing.T) {
		const once, twice = `version="1.4"`, `version="1.4" version="1.4"`
		if strings.Count(poisoned, once) != 1 {
			t.Fatalf("rendered definition does not contain %s exactly once; the control cannot be built", once)
		}
		malformed := strings.Replace(poisoned, once, twice, 1)
		err := task.Register(ctx, name, malformed)
		switch {
		case err == nil:
			t.Fatalf("Task Scheduler registered a definition with a repeated attribute")
		case strings.Contains(err.Error(), hrSchedMalformedXML):
			// The parse failure this test exists to catch is still detectable.
		case strings.Contains(err.Error(), hrSchedInvalidValue):
			t.Fatalf("a malformed definition read as a value error, so this test no longer discriminates: %v", err)
		default:
			t.Skipf("unclassified rejection of the malformed definition: %v", err)
		}
	})
}
