package acpagent

// grokSpecHooks is what grok's Spec registers from this package
// (internal/provider/grok/grok.go), for the tests here that exercise grok's
// handlers and operations: they cannot import the grok package, which imports
// this one. 0208 PLAN P6b moves those tests, and this list, to grok.
func grokSpecHooks() Spec {
	return Spec{
		ExtensionRequests: map[string]ExtensionRequestHandler{
			MethodGrokExitPlanMode:    HandleGrokExitPlanMode,
			MethodGrokAskUserQuestion: HandleGrokAskUserQuestion,
		},
		NewSessionState: NewGrokSessionState,
		InitializeMeta:  GrokInitializeMeta,
		Ops: Ops{
			Fork:          GrokFork,
			Compact:       GrokCompact,
			Rename:        GrokRename,
			UndoLast:      GrokUndoLast,
			RuntimeUsage:  GrokRuntimeUsage,
			RuntimeStatus: GrokRuntimeStatus,
			ConfirmLimit:  GrokConfirmLimit,
		},
	}
}

// bindGrok gives s what grok's Spec registers, as Start does.
func bindGrok(s *session) *session {
	(&Provider{spec: grokSpecHooks()}).bindSpec(s)
	return s
}
