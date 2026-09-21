package codex

import (
	"testing"
)

// TestEveryDeclaredNotificationIsRouted is acceptance criterion A3 of PLAN 0163
// and the structural fix for MADR 0163 F12.
//
// notificationRouteUnknown is the zero value of the route map, so a notification
// nobody added is dropped with a Debug log — indistinguishable from one that does
// not exist. Seven notifications sat in that state across six Codex releases, and
// two of them (modelProvider/authRecovery*) even had working handlers that
// nothing could reach.
//
// The expected set is read from the embedded contract manifest rather than a
// second hand-written list (PLAN 0163 C2): a list maintained beside the route
// table is exactly what drifted. Re-pinning the manifest therefore tightens this
// test automatically, with no edit here.
//
// This REPLACES TestRoutingClassifiesAllCodex01491Notifications, which was doing
// the same job and passing while seven notifications were unrouted. Three reasons
// it could not catch them, all worth remembering: it read
// testdata/0.149.1/manifest.json by a hardcoded path, so it asked about the wrong
// version; it hard-asserted exactly 75 notifications, so re-pinning would have
// broken it rather than tightening it; and it checked the stable surface only.
// This version is a strict superset on all three counts (PLAN 0163 C1).
//
// P6 re-pinned the manifest to 0.155.1, so this test now requires the seven
// notifications that release added. The explicit list that stood in for them
// between P2 and P6 has been deleted rather than maintained: one inventory, not
// two (C2).
func TestEveryDeclaredNotificationIsRouted(t *testing.T) {
	m, err := loadEmbeddedContractManifest()
	if err != nil {
		t.Fatalf("loading the embedded contract manifest: %v", err)
	}

	declared := map[string]string{}
	for label, surface := range map[string]ContractSurface{
		"stable":       m.Stable,
		"experimental": m.Experimental,
	} {
		for _, entry := range surface.ServerNotifications {
			// Prefer "stable" when a method appears in both surfaces, so the
			// failure message names the stronger obligation.
			if declared[entry.Method] == "" || label == "stable" {
				declared[entry.Method] = label
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("the manifest declares no notifications: this test would pass vacuously")
	}

	var missing []string
	for method, surface := range declared {
		if notificationRouteFor(method) == notificationRouteUnknown {
			missing = append(missing, method+" ("+surface+")")
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d of %d declared notification(s) have no route and would be silently dropped "+
			"at provider.go's notificationRouteUnknown branch:\n\t%v\n\n"+
			"Add each to notificationRoutes. Routing to notificationRouteProvider with an "+
			"explicit case is a valid answer for something we do not consume yet — a declared "+
			"drop is greppable, an unrouted one is not (MADR 0163 F12).",
			len(missing), len(declared), missing)
	}
}
