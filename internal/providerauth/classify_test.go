package providerauth_test

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// This is the regression guard for the assumption MADR 0074 D7 overturned.
//
// The four cases below are the *verbatim* authorize responses captured from
// kilo 7.4.20 on 2026-08-10 (MADR 0074 §7). All four carried
// `"method": "auto"` — which is why the classifier must never look at that
// field. Three are device flows the phone can complete; the fourth is a
// browser loopback it cannot. Getting this split wrong is not a cosmetic bug:
// a browser flow routed as a device flow shows a code that does not exist and
// hangs until it expires.
func TestClassifyRealAuthorizeResponses(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		instr    string
		want     providerauth.FlowKind
		wantCode string
	}{
		{
			name:     "kilo gateway device authorization",
			url:      "https://app.kilo.ai/device-auth?code=RX2Y-4H7X",
			instr:    "Open https://app.kilo.ai/device-auth?code=RX2Y-4H7X and enter code: RX2Y-4H7X",
			want:     providerauth.FlowDevice,
			wantCode: "RX2Y-4H7X",
		},
		{
			name:     "chatgpt pro/plus headless",
			url:      "https://auth.openai.com/codex/device",
			instr:    "Enter code: K5K8-L04UB",
			want:     providerauth.FlowDevice,
			wantCode: "K5K8-L04UB",
		},
		{
			name:     "github copilot",
			url:      "https://github.com/login/device",
			instr:    "Enter code: 8A31-10BC",
			want:     providerauth.FlowDevice,
			wantCode: "8A31-10BC",
		},
		{
			name: "chatgpt pro/plus browser (loopback)",
			url: "https://auth.openai.com/oauth/authorize?response_type=code" +
				"&client_id=app_EMoamEEZ73f0CkXaXp7hrann" +
				"&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback" +
				"&scope=openid+profile+email+offline_access" +
				"&code_challenge=JDScK1jSs7eP095X1IZKftlCHMxTIZRL_EGyM4F2MuE" +
				"&code_challenge_method=S256&id_token_add_organizations=true" +
				"&codex_cli_simplified_flow=true&state=Yu8139ZL711BZwSbOaJftIi0ZuFYrhYXbXMs4_aDQ8U" +
				"&originator=kilo",
			instr: "Complete authorization in your browser. This window will close automatically.",
			want:  providerauth.FlowBrowser,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := providerauth.Classify(tc.url, tc.instr)
			if got.Kind != tc.want {
				t.Fatalf("classified as %s, want %s", got.Kind, tc.want)
			}
			if got.UserCode != tc.wantCode {
				t.Errorf("user code = %q, want %q", got.UserCode, tc.wantCode)
			}
			if tc.want == providerauth.FlowDevice && got.VerificationURI == "" {
				t.Error("device flow has no verification URI to show")
			}
		})
	}
}

// The Gateway URL embeds the code. Showing it in the link would let anyone
// glancing at the screen read the code without reading the code field.
func TestClassifyStripsCodeFromVerificationURI(t *testing.T) {
	got := providerauth.Classify(
		"https://app.kilo.ai/device-auth?code=RX2Y-4H7X",
		"Open https://app.kilo.ai/device-auth?code=RX2Y-4H7X and enter code: RX2Y-4H7X",
	)
	if strings.Contains(got.VerificationURI, "RX2Y-4H7X") {
		t.Fatalf("verification URI still carries the code: %s", got.VerificationURI)
	}
	if !strings.HasPrefix(got.VerificationURI, "https://app.kilo.ai/device-auth") {
		t.Fatalf("verification URI mangled: %s", got.VerificationURI)
	}
	if got.UserCode != "RX2Y-4H7X" {
		t.Fatalf("code lost while stripping: %q", got.UserCode)
	}
}

// Any loopback spelling means the callback lands on the host.
func TestClassifyRecognisesEveryLoopbackSpelling(t *testing.T) {
	for _, redirect := range []string{
		"http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback",
		"http%3A%2F%2F127.0.0.1%3A8976%2Fcallback",
		"http%3A%2F%2F%5B%3A%3A1%5D%3A8080%2Fcb",
	} {
		got := providerauth.Classify(
			"https://vendor.example/oauth/authorize?redirect_uri="+redirect,
			"Complete authorization in your browser.",
		)
		if got.Kind != providerauth.FlowBrowser {
			t.Errorf("redirect %s classified as %s, want browser", redirect, got.Kind)
		}
	}
}

// A redirect to a real host is not a loopback flow; if it also carries a code
// it is usable from the phone.
func TestClassifyRemoteRedirectIsNotBrowserFlow(t *testing.T) {
	got := providerauth.Classify(
		"https://vendor.example/device?redirect_uri=https%3A%2F%2Fvendor.example%2Fdone",
		"Enter code: ABCD-1234",
	)
	if got.Kind != providerauth.FlowDevice {
		t.Fatalf("classified as %s, want device", got.Kind)
	}
}

// Neither a loopback redirect nor a code: refuse rather than guess. Guessing
// "device" would show an empty code box.
func TestClassifyUnknownWhenNothingUsable(t *testing.T) {
	got := providerauth.Classify("https://vendor.example/authorize", "Follow the instructions.")
	if got.Kind != providerauth.FlowUnknown {
		t.Fatalf("classified as %s, want unknown", got.Kind)
	}
	if got.UserCode != "" {
		t.Errorf("invented a code: %q", got.UserCode)
	}
}

// The code can arrive in the URL query rather than the prose.
func TestClassifyReadsCodeFromQuery(t *testing.T) {
	got := providerauth.Classify("https://vendor.example/device?user_code=WXYZ-9876", "Open the link.")
	if got.Kind != providerauth.FlowDevice || got.UserCode != "WXYZ-9876" {
		t.Fatalf("got %s/%q", got.Kind, got.UserCode)
	}
}

func TestClassifyHandlesUnparseableURL(t *testing.T) {
	got := providerauth.Classify("://not a url", "Enter code: ABCD-1234")
	if got.Kind != providerauth.FlowDevice {
		t.Fatalf("a bad URL with a good code should still be a device flow, got %s", got.Kind)
	}
}
