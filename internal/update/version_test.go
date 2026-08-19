package update

import "testing"

func TestParseBase(t *testing.T) {
	cases := []struct {
		in      string
		m, n, p int
		dev     bool
		wantErr bool
	}{
		{"0.6.7", 0, 6, 7, false, false},
		{"v0.6.7", 0, 6, 7, false, false},
		{"0.6.7.4.gf7fe252", 0, 6, 7, true, false},
		{"dev", 0, 0, 0, true, false},
		{"1.2", 0, 0, 0, false, true},
		{"0.13.9.1", 0, 13, 9, false, false},
		{"v0.13.9.1", 0, 13, 9, false, false},
		{"0.13.9.1.gdeadbee", 0, 13, 9, true, false},
		{"0.13.9.1.ci99", 0, 13, 9, true, false},
		{"debug", 0, 0, 0, true, false},
	}
	for _, tc := range cases {
		m, n, p, dev, err := ParseBase(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseBase(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBase(%q): %v", tc.in, err)
			continue
		}
		if m != tc.m || n != tc.n || p != tc.p || dev != tc.dev {
			t.Errorf("ParseBase(%q) = (%d,%d,%d,dev=%v), want (%d,%d,%d,dev=%v)",
				tc.in, m, n, p, dev, tc.m, tc.n, tc.p, tc.dev)
		}
	}
}

func TestParseVersionN(t *testing.T) {
	cases := []struct {
		in string
		n  int
	}{
		{"0.13.9", 0},
		{"0.13.9.1", 1},
		{"0.13.9.2", 2},
	}
	for _, tc := range cases {
		v, err := ParseVersion(tc.in)
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", tc.in, err)
			continue
		}
		if v.N != tc.n {
			t.Errorf("ParseVersion(%q).N = %d, want %d", tc.in, v.N, tc.n)
		}
		if v.Local {
			t.Errorf("ParseVersion(%q).Local = true, want false", tc.in)
		}
	}
}

func TestNewerBase(t *testing.T) {
	ok, err := NewerBase("0.6.8", "0.6.7")
	if err != nil || !ok {
		t.Fatalf("0.6.8 > 0.6.7: ok=%v err=%v", ok, err)
	}
	ok, err = NewerBase("0.6.7", "0.6.7")
	if err != nil || ok {
		t.Fatalf("equal: ok=%v err=%v", ok, err)
	}
	ok, err = NewerBase("0.6.6", "0.6.7")
	if err != nil || ok {
		t.Fatalf("older: ok=%v err=%v", ok, err)
	}
	ok, err = NewerBase("v0.7.0", "0.6.9.1.gabc")
	if err != nil || !ok {
		t.Fatalf("remote newer vs local dev: ok=%v err=%v", ok, err)
	}
}

func TestNewerPublished(t *testing.T) {
	cases := []struct {
		remote, local string
		newer         bool
	}{
		{"0.13.10.1", "0.13.9.1", true},
		{"0.13.9.2", "0.13.9.1", true},
		{"0.13.9.1", "0.13.9.1", false},
		{"0.13.9.1", "0.13.9", true},
		{"0.13.9.1", "0.13.9.1.gdead", false},
		{"0.13.9.1", "0.13.10.1", false},
	}
	for _, tc := range cases {
		got, err := NewerPublished(tc.remote, tc.local)
		if err != nil || got != tc.newer {
			t.Errorf("NewerPublished(%q,%q)=%v err=%v, want %v",
				tc.remote, tc.local, got, err, tc.newer)
		}
	}
}

func TestAssetFor(t *testing.T) {
	r := Release{
		Tag:  "v0.6.7",
		Base: "0.6.7",
		Assets: []Asset{
			{Name: "mcremote-darwin-arm64-0.6.7.4", URL: "http://x/a"},
			{Name: "SHA256SUMS-0.6.7.4", URL: "http://x/s"},
		},
	}
	a, ver, err := r.AssetFor("mcremote", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "mcremote-darwin-arm64-0.6.7.4" || ver != "0.6.7.4" {
		t.Fatalf("got %s ver=%s", a.Name, ver)
	}
	if _, _, err := r.AssetFor("mcrelay", "linux", "amd64"); err == nil {
		t.Fatal("expected missing asset error")
	}
}
