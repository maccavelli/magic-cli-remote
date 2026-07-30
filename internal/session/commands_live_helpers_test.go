//go:build live_grok

package session_test

import "testing"

// waitForNoticeContaining blocks until a notice event contains sub. Live
// tests need this because daemon notices can arrive after Prompt returns
// (pump goroutine), whereas unit tests often assert synchronously after a
// fake that emits under the call.
//
// Lives behind live_grok so staticcheck on the default test package does not
// report U1000 for a helper only used by live-tagged files.
func (s *eventSink) waitForNoticeContaining(t *testing.T, sub string) {
	t.Helper()
	waitFor(t, "notice containing "+sub, func() bool {
		return s.hasNoticeContaining(sub)
	})
}
