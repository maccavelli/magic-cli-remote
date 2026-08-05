package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatest(t *testing.T) {
	body := map[string]any{
		"tag_name": "v0.7.1",
		"assets": []map[string]any{
			{"name": "mcremote-linux-amd64-0.7.1.2", "browser_download_url": "http://x/a", "size": 10},
			{"name": "SHA256SUMS-0.7.1.2", "browser_download_url": "http://x/s", "size": 20},
		},
	}
	raw, _ := json.Marshal(body)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "" {
			t.Error("missing Accept header")
		}
		_, _ = w.Write(raw)
	}))
	defer ts.Close()

	rel, err := Latest(context.Background(), ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.7.1" || rel.Base != "0.7.1" {
		t.Fatalf("got tag=%s base=%s", rel.Tag, rel.Base)
	}
	if len(rel.Assets) != 2 {
		t.Fatalf("assets = %d", len(rel.Assets))
	}
}

func TestLatestHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer ts.Close()
	if _, err := Latest(context.Background(), ts.URL); err == nil {
		t.Fatal("expected error")
	}
}

func TestAssetForDuplicate(t *testing.T) {
	r := Release{
		Tag: "v0.6.7", Base: "0.6.7",
		Assets: []Asset{
			{Name: "mcremote-linux-amd64-0.6.7.1"},
			{Name: "mcremote-linux-amd64-0.6.7.2"},
		},
	}
	if _, _, err := r.AssetFor("mcremote", "linux", "amd64"); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestSumsAssetExact(t *testing.T) {
	r := Release{Assets: []Asset{
		{Name: "SHA256SUMS-0.6.7.4", URL: "http://s"},
		{Name: "SHA256SUMS", URL: "http://fallback"},
	}}
	a, err := r.SumsAsset("0.6.7.4")
	if err != nil || a.URL != "http://s" {
		t.Fatalf("got %+v err=%v", a, err)
	}
}
