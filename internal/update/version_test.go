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
