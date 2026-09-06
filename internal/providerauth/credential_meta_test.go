package providerauth

import (
	"testing"
	"time"
)

func TestFresher(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	cases := []struct {
		name     string
		cand     CredentialMeta
		existing CredentialMeta
		want     bool
	}{
		{
			name:     "different mode is never fresher",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 2},
			existing: CredentialMeta{Mode: "api_key", Sequence: 1},
			want:     false,
		},
		{
			name:     "later expiry is fresher",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t1},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			want:     true,
		},
		{
			name:     "equal expiry is not fresher",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			want:     false,
		},
		{
			name:     "earlier expiry is not fresher",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t1},
			want:     false,
		},
		{
			name:     "higher sequence is fresher when both nonzero",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 5},
			existing: CredentialMeta{Mode: "chatgpt", Sequence: 4},
			want:     true,
		},
		{
			name:     "equal sequence is not fresher",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 4},
			existing: CredentialMeta{Mode: "chatgpt", Sequence: 4},
			want:     false,
		},
		{
			name:     "zero sequence on either side refuses to guess",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 5},
			existing: CredentialMeta{Mode: "chatgpt", Sequence: 0},
			want:     false,
		},
		{
			name:     "no expiry and no sequence refuses to guess",
			cand:     CredentialMeta{Mode: "chatgpt"},
			existing: CredentialMeta{Mode: "chatgpt"},
			want:     false,
		},
		{
			name:     "expiry comparison preferred over sequence when both set",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t1, Sequence: 1},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t0, Sequence: 99},
			want:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cand.Fresher(tc.existing); got != tc.want {
				t.Fatalf("Fresher = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNotOlder(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	cases := []struct {
		name     string
		cand     CredentialMeta
		existing CredentialMeta
		want     bool
	}{
		{
			name:     "different mode is older-or-incomparable",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 2},
			existing: CredentialMeta{Mode: "api_key", Sequence: 1},
			want:     false,
		},
		{
			name:     "equal expiry is not older",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			want:     true,
		},
		{
			name:     "later expiry is not older",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t1},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			want:     true,
		},
		{
			name:     "earlier expiry is older",
			cand:     CredentialMeta{Mode: "chatgpt", ExpiresAt: t0},
			existing: CredentialMeta{Mode: "chatgpt", ExpiresAt: t1},
			want:     false,
		},
		{
			name:     "equal sequence is not older",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 4},
			existing: CredentialMeta{Mode: "chatgpt", Sequence: 4},
			want:     true,
		},
		{
			name:     "higher sequence is not older",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 5},
			existing: CredentialMeta{Mode: "chatgpt", Sequence: 4},
			want:     true,
		},
		{
			name:     "lower sequence is older",
			cand:     CredentialMeta{Mode: "chatgpt", Sequence: 3},
			existing: CredentialMeta{Mode: "chatgpt", Sequence: 4},
			want:     false,
		},
		{
			name:     "no signal refuses to guess",
			cand:     CredentialMeta{Mode: "chatgpt"},
			existing: CredentialMeta{Mode: "chatgpt"},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cand.NotOlder(tc.existing); got != tc.want {
				t.Fatalf("NotOlder = %v, want %v", got, tc.want)
			}
		})
	}
}
