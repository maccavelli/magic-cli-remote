package codex

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	wsJWTIssuer   = "mcremote"
	wsJWTAudience = "codex-app-server"
)

type webSocketAuth struct {
	secretFile string
	bearer     string
	header     http.Header
}

func createWebSocketAuth(runtimeDir string, mode WSAuthMode) (*webSocketAuth, error) {
	if runtimeDir == "" {
		return nil, fmt.Errorf("WebSocket auth requires the daemon runtime directory")
	}
	dir := filepath.Join(runtimeDir, "codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create Codex runtime directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure Codex runtime directory: %w", err)
	}

	random := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return nil, fmt.Errorf("generate WebSocket bearer material: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(random)
	file, err := os.CreateTemp(dir, "ws-secret-*")
	if err != nil {
		return nil, fmt.Errorf("create WebSocket secret file: %w", err)
	}
	path := file.Name()
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure WebSocket secret file: %w", err)
	}
	if _, err := file.WriteString(secret + "\n"); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write WebSocket secret file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close WebSocket secret file: %w", err)
	}

	bearer := secret
	switch mode {
	case WSAuthCapabilityToken:
	case WSAuthSignedBearer:
		bearer, err = signWebSocketBearer([]byte(secret), time.Now().UTC())
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported WebSocket auth mode %q", mode)
	}
	removeOnError = false
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+bearer)
	return &webSocketAuth{secretFile: path, bearer: bearer, header: header}, nil
}

func signWebSocketBearer(secret []byte, now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"iss": wsJWTIssuer,
		"aud": wsJWTAudience,
		"nbf": now.Add(-5 * time.Second).Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *webSocketAuth) remove() {
	if a != nil && a.secretFile != "" {
		_ = os.Remove(a.secretFile)
	}
}

func waitWebSocketHealth(ctx context.Context, baseURL string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	delay := 25 * time.Millisecond
	var lastErr error
	for {
		ready := true
		for _, path := range []string{"/readyz", "/healthz"} {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				lastErr = err
				ready = false
				break
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("%s returned HTTP %d", path, resp.StatusCode)
				ready = false
				break
			}
		}
		if ready {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr != nil {
				return fmt.Errorf("Codex WebSocket readiness: %w", lastErr)
			}
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 400*time.Millisecond {
			delay *= 2
		}
	}
}
