package opencode

import "testing"

// TestCatalogUpdatedInvalidatesTheCache is MADR 0137 F8.
//
// `catalog.updated` is opencode's own announcement that its model list changed.
// It was decoded and dropped, so a catalog memoized at engine boot outlived
// every upstream change for the life of the process, and the phone was offered
// models the engine would no longer accept.
func TestCatalogUpdatedInvalidatesTheCache(t *testing.T) {
	d := &httpDialect{}
	if !d.EngineEventInvalidatesCatalog("catalog.updated") {
		t.Fatal("catalog.updated must invalidate the model catalog")
	}
}

// TestPluginAddedDoesNotInvalidateTheCatalog is the other half, and it is the
// one worth pinning.
//
// `plugin.added` fired 45 times in one short turn on 1.18.26. It says nothing
// about models, and treating it as a catalog change would re-harvest a
// multi-MB payload 45 times for no new information — turning a noise finding
// into a performance defect.
func TestPluginAddedDoesNotInvalidateTheCatalog(t *testing.T) {
	d := &httpDialect{}
	for _, typ := range []string{
		"plugin.added", "message.updated", "session.idle", "mcp.tools.changed", "",
	} {
		if d.EngineEventInvalidatesCatalog(typ) {
			t.Errorf("%q must not invalidate the model catalog", typ)
		}
	}
}
