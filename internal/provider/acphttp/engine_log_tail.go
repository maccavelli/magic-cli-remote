package acphttp

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// gooseCLILogDir returns the directory goose serve writes structured JSON
// logs under (platform=cli). Goose does not mirror 429/quota failures to
// stderr — only to these files — so acphttp must tail them to surface
// silent provider backoff (MADR 0073 F1).
//
// Layout (goose 1.45, `goose info`):
//
//	$XDG_STATE_HOME/goose/logs/cli/YYYY-MM-DD/<HHMMSS>.log
//	or ~/.local/state/goose/logs/cli/…
func gooseCLILogDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "goose", "logs", "cli")
}

// startEngineFileLogTail watches provider-specific on-disk logs for limit
// signals the engine never writes to stderr. No-op for engines that only
// log to the captured stderr pipe.
func (p *Provider) startEngineFileLogTail(stop <-chan struct{}) {
	if p.spec.ID != provider.IDGoose {
		return
	}
	dir := gooseCLILogDir()
	if dir == "" {
		return
	}
	go p.tailGooseFileLogs(dir, stop)
}

// tailGooseFileLogs follows the newest .log under dir (including date
// subdirs), feeding complete lines into onEngineLogLine until stop closes.
// On first attach it seeks to EOF so historical sessions do not re-fire
// stale quota errors into a fresh engine.
func (p *Provider) tailGooseFileLogs(dir string, stop <-chan struct{}) {
	p.log.Info("tailing goose engine file logs", slog.String("dir", dir))

	var (
		curPath string
		offset  int64
		partial []byte
	)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}

		path, err := newestLogUnder(dir)
		if err != nil || path == "" {
			continue
		}
		if path != curPath {
			// New file (engine restart / day rollover / first discovery).
			// Start at EOF so we only surface errors from this process.
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			curPath = path
			offset = fi.Size()
			partial = nil
			p.log.Debug("following goose log file",
				slog.String("path", path),
				slog.Int64("offset", offset),
			)
			if hook := p.gooseTailAttached; hook != nil {
				hook(path, offset)
			}
			continue
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		size := fi.Size()
		if size < offset {
			// Truncated or rotated in place.
			offset = 0
			partial = nil
		}
		if size == offset {
			f.Close()
			continue
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			continue
		}
		chunk, err := io.ReadAll(io.LimitReader(f, 1<<20))
		f.Close()
		if err != nil || len(chunk) == 0 {
			continue
		}
		offset += int64(len(chunk))
		partial = append(partial, chunk...)

		for {
			i := bytes.IndexByte(partial, '\n')
			if i < 0 {
				// Cap pathological no-newline growth.
				if len(partial) > 64<<10 {
					line := string(partial)
					partial = nil
					p.onEngineLogLine(line)
				}
				break
			}
			line := strings.TrimSpace(string(partial[:i]))
			partial = partial[i+1:]
			if line != "" {
				p.onEngineLogLine(line)
			}
		}
	}
}

// newestLogUnder returns the path of the most recently modified .log under
// root, including one level of date subdirectories (goose's cli/YYYY-MM-DD/).
func newestLogUnder(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var (
		best     string
		bestTime time.Time
	)
	consider := func(path string, mod time.Time) {
		if !strings.HasSuffix(path, ".log") {
			return
		}
		if mod.After(bestTime) {
			bestTime = mod
			best = path
		}
	}
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		if e.IsDir() {
			sub, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, s := range sub {
				if s.IsDir() {
					continue
				}
				info, err := s.Info()
				if err != nil {
					continue
				}
				consider(filepath.Join(path, s.Name()), info.ModTime())
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		consider(path, info.ModTime())
	}
	return best, nil
}
