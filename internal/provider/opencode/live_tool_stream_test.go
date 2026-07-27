//go:build live_opencode

package opencode_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

type statusTransition struct {
	FrameIndex int    `json:"frame_index"`
	Status     string `json:"status"`
}

type toolCallCapture struct {
	Shape               string                      `json:"shape"`
	CallID              string                      `json:"call_id"`
	ToolName            string                      `json:"tool_name"`
	TotalFrames         int                         `json:"total_frames"`
	DistinctStatuses    []string                    `json:"distinct_statuses"`
	StatusTransitions   []statusTransition          `json:"status_transitions"`
	OutputLengths       []int                       `json:"output_lengths"`
	OutputMonotonic     bool                        `json:"output_monotonic"`
	TitleChangedMidRun  bool                        `json:"title_changed_mid_run"`
	InputChangedMidRun  bool                        `json:"input_changed_mid_run"`
	TerminalIsLastFrame bool                        `json:"terminal_is_last_frame"`
	Frames              []opencode.RawToolPartFrame `json:"frames"`
}

type toolFramesSpike struct {
	CLIVersion   string            `json:"cli_version"`
	CapturedAt   string            `json:"captured_at"`
	ToolCaptures []toolCallCapture `json:"tool_captures"`
}

func TestLiveToolStreamDynamics(t *testing.T) {
	var mu sync.Mutex
	var rawFrames []opencode.RawToolPartFrame

	hook := func(frame opencode.RawToolPartFrame) {
		mu.Lock()
		rawFrames = append(rawFrames, frame)
		mu.Unlock()
	}

	p := opencode.NewHTTPWithToolFrameHook(opencode.Config{AlwaysApprove: true}, nil, hook)
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	// 1. Prepare files for read and grep tool shapes.
	largeFilePath := filepath.Join(tmpDir, "large_file.txt")
	var largeFileLines []string
	for i := 1; i <= 500; i++ {
		largeFileLines = append(largeFileLines, fmt.Sprintf("Line %d: The quick brown fox jumps over the lazy dog %d", i, i))
	}
	if err := os.WriteFile(largeFilePath, []byte(strings.Join(largeFileLines, "\n")), 0644); err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}

	grepDir := filepath.Join(tmpDir, "grep_dir")
	if err := os.MkdirAll(grepDir, 0755); err != nil {
		t.Fatalf("failed to create grep dir: %v", err)
	}
	for f := 1; f <= 5; f++ {
		var content []string
		for l := 1; l <= 40; l++ {
			content = append(content, fmt.Sprintf("File %d line %d: TARGET_SEARCH_PATTERN match entry %d_%d", f, l, f, l))
		}
		if err := os.WriteFile(filepath.Join(grepDir, fmt.Sprintf("file_%d.txt", f)), []byte(strings.Join(content, "\n")), 0644); err != nil {
			t.Fatalf("failed to write grep file: %v", err)
		}
	}

	s, err := p.Start(ctx, provider.StartOptions{Name: "tool-stream-probe", CWD: tmpDir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	var captures []toolCallCapture

	runPromptAndCollect := func(shape string, promptText string) {
		mu.Lock()
		startIdx := len(rawFrames)
		mu.Unlock()

		if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: promptText}}); err != nil {
			t.Fatalf("[%s] prompt error: %v", shape, err)
		}

		deadline := time.After(120 * time.Second)
		var sawComplete bool
		for !sawComplete {
			select {
			case ev, ok := <-s.Events():
				if !ok {
					t.Fatalf("[%s] events channel closed prematurely", shape)
				}
				if ev.Type == event.TypeTurnComplete {
					sawComplete = true
				} else if ev.Type == event.TypeError {
					t.Fatalf("[%s] agent error: %s", shape, ev.Error)
				}
			case <-deadline:
				t.Fatalf("[%s] timeout waiting for turn completion", shape)
			}
		}

		mu.Lock()
		turnFrames := make([]opencode.RawToolPartFrame, len(rawFrames)-startIdx)
		copy(turnFrames, rawFrames[startIdx:])
		mu.Unlock()

		// Group frames by CallID (or PartID if CallID is empty).
		framesByCall := make(map[string][]opencode.RawToolPartFrame)
		var callOrder []string
		for _, f := range turnFrames {
			cid := f.CallID
			if cid == "" {
				cid = f.PartID
			}
			if cid == "" {
				continue
			}
			if _, exists := framesByCall[cid]; !exists {
				callOrder = append(callOrder, cid)
			}
			framesByCall[cid] = append(framesByCall[cid], f)
		}

		for _, cid := range callOrder {
			cCallFrames := framesByCall[cid]
			cap := analyzeCallFrames(shape, cid, cCallFrames)
			captures = append(captures, cap)

			// Assertions pinning the design facts (MADR 0034 Phase 0 Step 4):
			if !cap.OutputMonotonic {
				t.Errorf("[%s call %s] len(state.output) was not non-decreasing across frames: %v", shape, cid, cap.OutputLengths)
			}
			if !cap.TerminalIsLastFrame {
				t.Errorf("[%s call %s] terminal status was not the last frame (statuses: %v)", shape, cid, cap.DistinctStatuses)
			}
		}
	}

	// 1. Bash tool shape: command with incremental output.
	runPromptAndCollect("bash", "Execute this bash command: for i in $(seq 1 10); do echo \"line $i\"; sleep 0.2; done")

	// 2. Read tool shape: read a large file.
	runPromptAndCollect("read", fmt.Sprintf("Read the entire file at %s using the read tool.", largeFilePath))

	// 3. Grep tool shape: search for pattern across files.
	runPromptAndCollect("grep", fmt.Sprintf("Search for 'TARGET_SEARCH_PATTERN' in directory %s using grep.", grepDir))

	spikeData := toolFramesSpike{
		CLIVersion:   "1.18.5",
		CapturedAt:   time.Now().UTC().Format(time.RFC3339),
		ToolCaptures: captures,
	}

	// Save capture into docs/opencode-spike-1.18.5/tool-frames.json
	repoRoot := filepath.Join("..", "..", "..")
	outDir := filepath.Join(repoRoot, "docs", "opencode-spike-1.18.5")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatalf("failed to create spike doc dir: %v", err)
	}

	outFile := filepath.Join(outDir, "tool-frames.json")
	data, err := json.MarshalIndent(spikeData, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal spike json: %v", err)
	}

	if err := os.WriteFile(outFile, data, 0644); err != nil {
		t.Fatalf("failed to write %s: %v", outFile, err)
	}

	t.Logf("Successfully captured Phase 0 tool frames spike to %s (%d tool call captures)", outFile, len(captures))
}

func analyzeCallFrames(shape string, callID string, frames []opencode.RawToolPartFrame) toolCallCapture {
	cap := toolCallCapture{
		Shape:               shape,
		CallID:              callID,
		TotalFrames:         len(frames),
		OutputMonotonic:     true,
		TerminalIsLastFrame: true,
		Frames:              frames,
	}

	if len(frames) > 0 {
		cap.ToolName = frames[0].Tool
		if cap.ToolName == "" {
			cap.ToolName = frames[0].Title
		}
	}

	firstTitle := ""
	firstInput := ""
	lastOutLen := -1

	seenStatusMap := make(map[string]bool)

	for i, f := range frames {
		if !seenStatusMap[f.Status] {
			seenStatusMap[f.Status] = true
			cap.DistinctStatuses = append(cap.DistinctStatuses, f.Status)
			cap.StatusTransitions = append(cap.StatusTransitions, statusTransition{
				FrameIndex: i,
				Status:     f.Status,
			})
		}

		outLen := len(f.Output)
		cap.OutputLengths = append(cap.OutputLengths, outLen)
		if lastOutLen >= 0 && outLen < lastOutLen {
			cap.OutputMonotonic = false
		}
		lastOutLen = outLen

		if i == 0 {
			firstTitle = f.Title
			firstInput = f.Input
		} else {
			if f.Title != firstTitle {
				cap.TitleChangedMidRun = true
			}
			if f.Input != firstInput {
				cap.InputChangedMidRun = true
			}
		}

		// Check terminal status rule: if terminal status appears, it must be the last frame
		if f.Status == "completed" || f.Status == "error" || f.Status == "failed" {
			if i != len(frames)-1 {
				cap.TerminalIsLastFrame = false
			}
		}
	}

	return cap
}
