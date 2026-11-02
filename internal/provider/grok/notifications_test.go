package grok

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// From acpagent/subagents_test.go.

// The three payloads below are captured verbatim from grok 0.2.114 driving a
// sub-agent that read two files (MADR 0051 §10.2).
func TestXAISubagentLifecycle(t *testing.T) {
	s, a := startSession(t, Config{}, acptest.Options{})
	prompt := startTurn(t, s, a)

	spawned := `{"sessionId":"agent-1","update":{
		"sessionUpdate":"subagent_spawned",
		"subagent_id":"019fae56-077f","parent_session_id":"agent-1",
		"child_session_id":"019fae56-077f","subagent_type":"explore",
		"description":"Read a.txt and b.txt words","role":"explore",
		"model":"grok-4.5","capability_mode":"read-only"}}`
	progress := `{"sessionId":"agent-1","update":{
		"sessionUpdate":"subagent_progress","subagent_id":"019fae56-077f",
		"duration_ms":2498,"turn_count":1,"tokens_used":1457}}`
	finished := `{"sessionId":"agent-1","update":{
		"sessionUpdate":"subagent_finished","subagent_id":"019fae56-077f",
		"status":"completed","tool_calls":2,"turns":1,"duration_ms":4929,
		"output":"## Summary\n\nhello and world"}}`

	ctx := context.Background()
	handleSessionNotification(ctx, s, json.RawMessage(spawned))
	evs := acptest.Drain(s)
	if len(evs) != 1 || evs[0].Type != event.TypeSubagents {
		t.Fatalf("spawned emitted %v, want one subagents event", evs)
	}
	set := evs[0].Subagents
	if len(set) != 1 {
		t.Fatalf("set=%v, want one entry", set)
	}
	if set[0].ID != "019fae56-077f" || set[0].Name != "explore" ||
		set[0].Task != "Read a.txt and b.txt words" ||
		set[0].Status != event.SubagentStatusRunning {
		t.Fatalf("entry=%+v", set[0])
	}

	handleSessionNotification(ctx, s, json.RawMessage(progress))
	if evs := acptest.Drain(s); len(evs) != 1 {
		t.Fatalf("progress emitted %v, want one refresh", evs)
	}

	handleSessionNotification(ctx, s, json.RawMessage(finished))
	evs = acptest.Drain(s)
	if len(evs) != 1 {
		t.Fatalf("finished emitted %v", evs)
	}
	if got := evs[0].Subagents[0].Status; got != event.SubagentStatusCompleted {
		t.Fatalf("status=%q, want completed", got)
	}
	// The sub-agent's own report is exactly what stays out of the transcript.
	if evs[0].Subagents[0].Task == "" ||
		evs[0].Text != "" || evs[0].Subagents[0].Name == "" {
		t.Fatalf("unexpected payload: %+v", evs[0])
	}

	// A late progress frame must not reopen a finished agent.
	handleSessionNotification(ctx, s, json.RawMessage(progress))
	if evs := acptest.Drain(s); len(evs) != 0 {
		t.Fatalf("late progress reopened a finished sub-agent: %v", evs)
	}

	// Turn end clears the panel.
	a.Respond(prompt, map[string]any{"stopReason": "end_turn"})
	var clears []event.Event
	evs = eventsUntil(t, s, event.TypeTurnComplete)
	for _, ev := range evs {
		if ev.Type == event.TypeSubagents {
			clears = append(clears, ev)
		}
	}
	if len(clears) != 1 || len(clears[0].Subagents) != 0 {
		t.Fatalf("turn end emitted %v, want one empty subagents event", evs)
	}
}

func TestXAISubagentFailedStatus(t *testing.T) {
	s := newSession(t, "agent-1")
	handleSessionNotification(context.Background(), s, json.RawMessage(
		`{"sessionId":"agent-1","update":{"sessionUpdate":"subagent_finished",
		  "subagent_id":"x","subagent_type":"explore","status":"interrupted"}}`))
	evs := acptest.Drain(s)
	if len(evs) != 1 || evs[0].Subagents[0].Status != event.SubagentStatusFailed {
		t.Fatalf("emitted %v, want failed", evs)
	}
}

// Other variants share this channel — tool_call_delta_chunk and turn_completed
// arrive here too, including for the child. None of them is status.
func TestXAINonSubagentVariantsIgnored(t *testing.T) {
	s := newSession(t, "agent-1")
	ctx := context.Background()
	for _, frame := range []string{
		`{"sessionId":"agent-1","update":{"sessionUpdate":"tool_call_delta_chunk","tool_call_id":"c","name":"read_file"}}`,
		`{"sessionId":"agent-1","update":{"sessionUpdate":"turn_completed","stop_reason":"end_turn"}}`,
		`{"sessionId":"agent-1","update":{"sessionUpdate":"model_changed","model_id":"grok-4.5"}}`,
		// A sub-agent frame addressed to the child, not to us.
		`{"sessionId":"child-9","update":{"sessionUpdate":"subagent_spawned","subagent_id":"z","subagent_type":"explore"}}`,
	} {
		handleSessionNotification(ctx, s, json.RawMessage(frame))
	}
	if evs := acptest.Drain(s); len(evs) != 0 {
		t.Fatalf("emitted %v for frames that carry no sub-agent status", evs)
	}
}

// retry_state on _x.ai/session_notification: a non-terminal retry with limit
// text mid-turn goes through the same abort machinery as stderr scraping —
// the structured analog of "Backing off for 3600s" stderr lines (MADR 0073).
func TestRetryStateNotificationAbortsTurn(t *testing.T) {
	s, a := startSession(t, Config{}, acptest.Options{})
	startTurn(t, s, a)

	frame := `{"sessionId":"agent-1","update":{"sessionUpdate":"retry_state","type":"retrying","error_type":"rate_limit","message":"request failed (attempt 1/3), retrying in 20s: 429 Too Many Requests"}}`
	handleSessionNotification(context.Background(), s, json.RawMessage(frame))

	evs := eventsUntil(t, s, event.TypeError)
	var errEv *event.Event
	for i := range evs {
		if evs[i].Type == event.TypeError {
			errEv = &evs[i]
		}
	}
	if errEv == nil {
		t.Fatalf("no error event; got %v", evs)
	}
	if errEv.ErrorKind != "rate_limit" {
		t.Fatalf("error_kind=%q, want rate_limit", errEv.ErrorKind)
	}
	if errEv.RetryAt.IsZero() {
		t.Fatal("retry_at should be parsed from 'retrying in 20s'")
	}
}

// A terminal retry_state ("failed") is deliberately NOT handled here: the
// session/prompt JSON-RPC error carrying the same cause arrives immediately
// after and owns the turn's error emission — handling both would double-post
// the failure into the chat.
func TestRetryStateFailedIsLeftToRPCError(t *testing.T) {
	s, a := startSession(t, Config{}, acptest.Options{})
	startTurn(t, s, a)

	frame := `{"sessionId":"agent-1","update":{"sessionUpdate":"retry_state","type":"failed","error_type":"auth","message":"Unauthorized (401) from upstream"}}`
	handleSessionNotification(context.Background(), s, json.RawMessage(frame))

	if evs := acptest.Drain(s); len(evs) != 0 {
		t.Fatalf("terminal retry_state must emit nothing (RPC error owns it); got %v", evs)
	}
}

// From acpagent/xaicompaction_test.go.

// xaiFrame is one `_x.ai/session_notification` as grok serializes it
// (xai-grok-shell extensions/notification.rs, `tag = "sessionUpdate"`,
// snake_case fields).
func xaiFrame(session, update string) json.RawMessage {
	return json.RawMessage(`{"sessionId":"` + session + `","update":` + update + `}`)
}

// TestXAIManualCompactShowsACompaction is the regression for MADR 0208's P4b
// deviation 1. grok's `/compact` compacts, then reports it with a lone
// auto_compact_completed (compaction.rs run_compact: no started before it).
// Magic dropped the variant, so the turn looked silent and `/compact` was
// about to be marked unavailable.
func TestXAIManualCompactShowsACompaction(t *testing.T) {
	s := newHostSession(t)
	handleSessionNotification(context.Background(), s, xaiFrame("agent-q",
		`{"sessionUpdate":"auto_compact_completed","tokens_before":42000,"tokens_after":9000}`))

	got := drainCompactions(s)
	want := []compactionState{{ID: "xai-1", Status: "completed"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("compactions = %+v, want %+v", got, want)
	}
}

// TestXAIAutoCompactFoldsIntoOneCompaction covers grok's automatic path:
// started opens the compaction, and the terminal variant closes the same one.
func TestXAIAutoCompactFoldsIntoOneCompaction(t *testing.T) {
	cases := []struct {
		name     string
		terminal string
		want     compactionState
	}{
		{"completed",
			`{"sessionUpdate":"auto_compact_completed","tokens_after":9000,"elapsed_ms":812,"summary_preview":" grok's own words "}`,
			compactionState{ID: "xai-1", Status: "completed", Summary: "grok's own words"}},
		{"failed",
			`{"sessionUpdate":"auto_compact_failed","error":" context too large "}`,
			compactionState{ID: "xai-1", Status: "failed", Error: "context too large"}},
		{"cancelled",
			`{"sessionUpdate":"auto_compact_cancelled","reason":"user_cancelled"}`,
			compactionState{ID: "xai-1", Status: "cancelled"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newHostSession(t)
			handleSessionNotification(context.Background(), s, xaiFrame("agent-q",
				`{"sessionUpdate":"auto_compact_started","tokens_used":200000,`+
					`"context_window":256000,"percentage":82,"reason":"Context window 82% full"}`))
			handleSessionNotification(context.Background(), s, xaiFrame("agent-q", tc.terminal))

			got := drainCompactions(s)
			if len(got) != 2 {
				t.Fatalf("got %d compaction events, want 2: %+v", len(got), got)
			}
			if started := (compactionState{ID: "xai-1", Status: "in_progress"}); got[0] != started {
				t.Fatalf("first event = %+v, want %+v", got[0], started)
			}
			if got[1] != tc.want {
				t.Fatalf("terminal event = %+v, want %+v", got[1], tc.want)
			}
		})
	}
}

// TestXAICompactionsAreNumbered proves a closed compaction is not reopened: the
// next one grok reports is a new compaction, so the phone shows two.
func TestXAICompactionsAreNumbered(t *testing.T) {
	s := newHostSession(t)
	for _, u := range []string{
		`{"sessionUpdate":"auto_compact_started","reason":"x"}`,
		`{"sessionUpdate":"auto_compact_cancelled","reason":"user_cancelled"}`,
		`{"sessionUpdate":"auto_compact_completed","tokens_after":1}`,
	} {
		handleSessionNotification(context.Background(), s, xaiFrame("agent-q", u))
	}
	got := drainCompactions(s)
	if len(got) != 3 || got[1].ID != "xai-1" || got[2].ID != "xai-2" {
		t.Fatalf("compactions = %+v, want xai-1 twice then xai-2", got)
	}
}

// TestXAICompactionOfAnotherSessionIsNotShown keeps the channel's origin rule:
// a sub-agent's frames arrive here too, and its compaction is not this one.
func TestXAICompactionOfAnotherSessionIsNotShown(t *testing.T) {
	s := newHostSession(t)
	handleSessionNotification(context.Background(), s, xaiFrame("some-child",
		`{"sessionUpdate":"auto_compact_completed","tokens_after":1}`))
	if got := drainCompactions(s); len(got) != 0 {
		t.Fatalf("compactions = %+v for another session, want none", got)
	}
}

// From acpagent/xaicost_test.go.

// usdOf is how a tick total must reach the wire: grok's 1e10 ticks to the
// dollar, as a session cost in USD.
func usdOf(ticks int64) event.SessionCost {
	return event.SessionCost{Amount: float64(ticks) / usdTicksPerUSD, Currency: "USD"}
}

// liveTurn is a live `_x.ai/session_notification` turn_completed. An empty
// promptID leaves the field out; a nil ticks leaves the cost out.
func liveTurn(t *testing.T, s testSession, promptID string, ticks *int64, partial, replay bool) {
	t.Helper()
	usage := map[string]any{"totalTokens": 10, "inputTokens": 8, "outputTokens": 2}
	if ticks != nil {
		usage["costUsdTicks"] = *ticks
	}
	if partial {
		usage["costIsPartial"] = true
	}
	update := map[string]any{"sessionUpdate": "turn_completed", "usage": usage}
	if promptID != "" {
		update["prompt_id"] = promptID
	}
	params := map[string]any{"sessionId": s.AgentSessionID(), "update": update}
	if replay {
		params["_meta"] = map[string]any{"isReplay": true}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	handleSessionNotification(context.Background(), s, raw)
}

// drainEvents returns every event queued so far.
func drainEvents(s testSession) []event.Event {
	return acptest.Drain(s)
}

// TestReplayedTurnCostsRebuildTheSessionTotal is 0199 E1 and E2 against grok's
// own frames: a session/load replays every turn on `_x.ai/session/update`, each
// with its original cost. Those add to the sum and report nothing; the first
// live turn reports the whole session's total.
func TestReplayedTurnCostsRebuildTheSessionTotal(t *testing.T) {
	f, err := os.Open("testdata/wire/1.0.50/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := &replayHost{testSession: newHostSession(t), loading: true}
	replayed := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var frame struct {
			Method string `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &frame) != nil || frame.Method != "_x.ai/session/update" {
			continue
		}
		s.id = frame.Params.SessionID
		var params struct {
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &params); err != nil {
			t.Fatal(err)
		}
		handleSessionUpdate(context.Background(), s, params.Params)
		replayed++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if replayed != 3 {
		t.Fatalf("replayed %d `_x.ai/session/update` frames, want the capture's 3", replayed)
	}
	if evs := drainEvents(s); len(evs) != 0 {
		t.Fatalf("the replay channel emitted %d events, want none: %+v", len(evs), evs)
	}

	// A turn grok marks as a replay on the live channel adds and reports no
	// session cost either, even after loading has ended.
	s.loading = false
	liveTurn(t, s, "replayed-elsewhere", new(int64(5)), false, true)
	if u := drainUsage(t, s); u.SessionCost != nil {
		t.Fatalf("a replay-marked turn reported session cost %+v", *u.SessionCost)
	}

	liveTurn(t, s, "live-1", new(int64(10_000_000)), false, false)
	u := drainUsage(t, s)
	want := usdOf(118_673_600 + 31_320_800 + 31_633_600 + 5 + 10_000_000)
	if u.SessionCost == nil || *u.SessionCost != want {
		t.Fatalf("SessionCost = %+v, want %+v: the replayed turns and the live one", u.SessionCost, want)
	}
	if u.CostUSD != nil {
		t.Fatalf("CostUSD = %v: grok's cost is a session total, never the per-turn figure (0189 D1)", *u.CostUSD)
	}
}

// TestARepeatedTurnCountsOnce is 0199 E1's dedupe by prompt_id.
func TestARepeatedTurnCountsOnce(t *testing.T) {
	s := newHostSession(t)
	liveTurn(t, s, "p1", new(int64(100)), false, false)
	_ = drainUsage(t, s)
	liveTurn(t, s, "p1", new(int64(100)), false, false)
	u := drainUsage(t, s)
	if u.SessionCost == nil || *u.SessionCost != usdOf(100) {
		t.Fatalf("SessionCost = %+v, want %+v: a repeated prompt_id counted twice", u.SessionCost, usdOf(100))
	}
}

// TestALiveTurnWithoutAPromptIDStillCounts: nothing can match such a turn, so
// it is counted rather than lost.
func TestALiveTurnWithoutAPromptIDStillCounts(t *testing.T) {
	s := newHostSession(t)
	liveTurn(t, s, "", new(int64(100)), false, false)
	_ = drainUsage(t, s)
	liveTurn(t, s, "", new(int64(100)), false, false)
	u := drainUsage(t, s)
	if u.SessionCost == nil || *u.SessionCost != usdOf(200) {
		t.Fatalf("SessionCost = %+v, want %+v", u.SessionCost, usdOf(200))
	}
}

// TestAnUnknownTurnCostStopsTheSessionTotal is 0199 E3: a total missing a turn
// is too low and would read as complete, so it is not reported at all. The
// token counts still are.
func TestAnUnknownTurnCostStopsTheSessionTotal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ticks   *int64
		partial bool
	}{
		{"no cost", nil, false},
		{"partial cost", new(int64(50)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newHostSession(t)
			liveTurn(t, s, "p1", new(int64(100)), false, false)
			if u := drainUsage(t, s); u.SessionCost == nil {
				t.Fatal("first turn reported no session cost")
			}
			liveTurn(t, s, "p2", tc.ticks, tc.partial, false)
			if u := drainUsage(t, s); u.SessionCost != nil || u.Used != 10 {
				t.Fatalf("turn with an unknown cost: SessionCost %+v, Used %d; want none and 10",
					u.SessionCost, u.Used)
			}
			liveTurn(t, s, "p3", new(int64(100)), false, false)
			if u := drainUsage(t, s); u.SessionCost != nil {
				t.Fatalf("a later turn reported %+v after an unknown cost", *u.SessionCost)
			}
		})
	}
}

// TestReplayChannelDeclinesOtherVariantsByName: `_x.ai/session/update` carries
// more than turns (a `background_tasks` in the 1.0.50 load capture). Those are
// declined by name, not silently dropped, and nothing is emitted.
func TestReplayChannelDeclinesOtherVariantsByName(t *testing.T) {
	s := &declineRecorder{testSession: newHostSession(t)}
	params := json.RawMessage(`{"sessionId":"agent-q","update":{"sessionUpdate":"background_tasks","tasks":[]},` +
		`"_meta":{"isReplay":true}}`)
	handleSessionUpdate(context.Background(), s, params)
	handleSessionUpdate(context.Background(), s, params)
	if evs := drainEvents(s); len(evs) != 0 {
		t.Fatalf("emitted %d events for background_tasks, want none", len(evs))
	}
	declined := s.got()
	if !slices.Contains(declined, "_x.ai/session/update.background_tasks") {
		t.Fatalf("background_tasks was not declined by name; declined = %v", declined)
	}
}

// From acpagent/xaiextensions_test.go.

// TestMCPServersUpdatedRecordsMembership is MADR 0137 F11.
//
// grok reports its full MCP membership on `_x.ai/mcp/servers_updated`. Unrouted,
// mcremote's server list came only from its own config, so a server grok added
// at runtime was invisible in /status.
func TestMCPServersUpdatedRecordsMembership(t *testing.T) {
	s := newHostSession(t)
	params := json.RawMessage(`{"mcpServers":[{"name":"magictools","source":"local",` +
		`"type":"http","url":"http://localhost:48080/mcp"}]}`)

	handleMCPServersUpdated(context.Background(), s, params)

	st := mcpStatus(t, s)
	if len(st) != 1 || st[0].Name != "magictools" {
		t.Fatalf("mcpStatus = %+v, want one entry named magictools", st)
	}
	// State is not invented. A server that has been named but has reported no
	// status is connecting, not ready — claiming it is up because it was
	// mentioned is exactly the guess /status must not make.
	if st[0].State != "connecting" {
		t.Fatalf("state = %q, want connecting: membership is not health",
			st[0].State)
	}
}

// TestMCPServersUpdatedDoesNotOverwriteKnownState proves a membership frame
// arriving after a status frame does not reset a server to "connecting".
func TestMCPServersUpdatedDoesNotOverwriteKnownState(t *testing.T) {
	s := newHostSession(t)
	handleMCPStatus(context.Background(), s,
		json.RawMessage(`{"name":"magictools","status":"ready"}`))
	handleMCPServersUpdated(context.Background(), s,
		json.RawMessage(`{"mcpServers":[{"name":"magictools"}]}`))

	st := mcpStatus(t, s)
	if len(st) != 1 {
		t.Fatalf("mcpStatus = %+v, want one entry (no duplicate)", st)
	}
	if st[0].State != "ready" {
		t.Fatalf("state = %q, want ready: a membership frame must not "+
			"overwrite observed health", st[0].State)
	}
}

// TestMCPInitProgressIsLoggedNotEmitted pins the placement decision.
//
// MCP startup is work between the prompt and the first token, so the counter
// is worth having where an operator investigating latency will find it. It is
// deliberately NOT a transcript event: promoting a progress counter to
// something the phone renders is a protocol and UI decision needing its own
// record.
func TestMCPInitProgressIsLoggedNotEmitted(t *testing.T) {
	s := newHostSession(t)
	handleMCPInitProgress(context.Background(), s,
		json.RawMessage(`{"total":3,"connected":1,"sessionId":"agent-q"}`))
	if n := countUsageEvents(s); n != 0 {
		t.Fatalf("progress produced %d events; it must not reach the transcript", n)
	}
	if evs := acptest.Drain(s); len(evs) != 0 {
		t.Fatalf("progress emitted %d events; it must not reach the transcript", len(evs))
	}
}

// TestMCPInitProgressHonoursTheOriginRule: a sub-agent's startup progress is
// not this conversation's, and the rest of this channel already enforces that.
func TestMCPInitProgressHonoursTheOriginRule(t *testing.T) {
	s := newHostSession(t)
	// Should be ignored entirely rather than panicking or being attributed here.
	handleMCPInitProgress(context.Background(), s,
		json.RawMessage(`{"total":3,"connected":1,"sessionId":"some-child"}`))
	if evs := acptest.Drain(s); len(evs) != 0 {
		t.Fatalf("a child's progress produced %d events", len(evs))
	}
}

// From acpagent/xaiusage_test.go.

// TestXAITurnCompletedCarriesGroksCacheSplit is the regression for the one
// provider whose cold/warm signal had nowhere to live (MADR 0137, second
// correction).
//
// `acpagent` maps ACP's SessionUsageUpdate, whose schema is {used, size, cost}
// and carries no cache fields at all (acp-go-sdk@v0.13.5 types_gen.go:5243).
// grok's token accounting travels on the `turn_completed` variant of
// `_x.ai/session_notification`, which this handler previously ignored — so
// every grok turn was recorded as having paid a full uncached prefill,
// including the ones that read 11776 cached tokens.
func TestXAITurnCompletedCarriesGroksCacheSplit(t *testing.T) {
	s := newHostSession(t)
	params := json.RawMessage(`{"sessionId":"agent-q","update":{` +
		`"sessionUpdate":"turn_completed","stop_reason":"end_turn",` +
		`"usage":{"inputTokens":19042,"outputTokens":48,"totalTokens":19090,` +
		`"cachedReadTokens":5888,"cacheCreationTokens":0,"reasoningTokens":34}}}`)

	handleSessionNotification(context.Background(), s, params)

	u := drainUsage(t, s)
	if u.CacheRead != 5888 {
		t.Fatalf("CacheRead = %d, want 5888: grok's warm turns are invisible", u.CacheRead)
	}
	if u.Used != 19090 || u.Input != 19042 || u.Output != 48 || u.Reasoning != 34 {
		t.Fatalf("token split wrong: %+v", u)
	}
}

// TestXAIUsageIsNotPublishedForAnotherSession keeps the origin rule that the
// rest of this handler already enforces: a sub-agent's frames arrive on the
// same channel, and its token accounting is not this conversation's.
func TestXAIUsageIsNotPublishedForAnotherSession(t *testing.T) {
	s := newHostSession(t)
	params := json.RawMessage(`{"sessionId":"some-child","update":{` +
		`"sessionUpdate":"turn_completed",` +
		`"usage":{"totalTokens":19090,"cachedReadTokens":5888}}}`)

	handleSessionNotification(context.Background(), s, params)

	if n := countUsageEvents(s); n != 0 {
		t.Fatalf("got %d usage events for a different session's turn, want 0", n)
	}
}

// TestXAIUsageIgnoresAnEmptyReport proves a turn_completed without usage is
// not turned into a zero report, which would read as a real measurement.
func TestXAIUsageIgnoresAnEmptyReport(t *testing.T) {
	s := newHostSession(t)
	handleSessionNotification(context.Background(), s,
		json.RawMessage(`{"sessionId":"agent-q","update":{"sessionUpdate":"turn_completed"}}`))
	if n := countUsageEvents(s); n != 0 {
		t.Fatalf("got %d usage events for a turn reporting none, want 0", n)
	}
}

// TestFixtureConfirmsGrokReportsACacheRead reads the claim from grok's own
// wire rather than assuming it.
func TestFixtureConfirmsGrokReportsACacheRead(t *testing.T) {
	data, err := os.ReadFile("testdata/wire/1.0.13/frames.jsonl")
	if err != nil {
		t.Skipf("grok fixture unavailable: %v", err)
	}
	if !strings.Contains(string(data), `"cachedReadTokens"`) {
		t.Fatal("no cachedReadTokens in grok's fixture: the premise of this " +
			"mapping is that grok reports one")
	}
}

func drainUsage(t *testing.T, s testSession) *event.Usage {
	t.Helper()
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeUsage && ev.Usage != nil {
				return ev.Usage
			}
		default:
			t.Fatal("no usage event emitted")
			return nil
		}
	}
}

func countUsageEvents(s testSession) int {
	n := 0
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeUsage {
				n++
			}
		default:
			return n
		}
	}
}
