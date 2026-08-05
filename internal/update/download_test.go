package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadVerified_OK(t *testing.T) {
	body := []byte("hello-binary")
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	sums := hexSum + "  mcremote-linux-amd64-0.1.0\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	dir := t.TempDir()
	staged, err := DownloadVerified(context.Background(),
		Asset{Name: "mcremote-linux-amd64-0.1.0", URL: ts.URL + "/bin"},
		Asset{Name: "SHA256SUMS-0.1.0", URL: ts.URL + "/sums"},
		dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("content mismatch")
	}
}

func TestDownloadVerified_Mismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("deadbeef  mcremote-linux-amd64-0.1.0\n"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	dir := t.TempDir()
	_, err := DownloadVerified(context.Background(),
		Asset{Name: "mcremote-linux-amd64-0.1.0", URL: ts.URL + "/bin"},
		Asset{Name: "SHA256SUMS", URL: ts.URL + "/sums"},
		dir, nil)
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
	// Staged file must not remain; destination dir otherwise empty.
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(matches) != 0 {
		t.Fatalf("files left after mismatch: %v", matches)
	}
}

func TestDownloadVerified_CustomVerifier(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ignored"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	dir := t.TempDir()
	called := false
	_, err := DownloadVerified(context.Background(),
		Asset{Name: "a", URL: ts.URL + "/bin"},
		Asset{Name: "s", URL: ts.URL + "/sums"},
		dir,
		func(staged, name, sums string) error {
			called = true
			return nil
		})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
