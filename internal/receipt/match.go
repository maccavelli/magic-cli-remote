package receipt

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// ShouldReceipt reports whether a resolved permission decision for toolName
// and detail should get a signed receipt, per cfg (MADR 0077 D4/D10).
// Disabled configs always return false. A deny match wins over an allow
// match on the same input — an operator excluding something takes priority
// over a broader allow rule that happens to also match.
//
// A malformed pattern is treated as non-matching and logged at Warn with the
// offending pattern: a typo'd pattern degrading to "no receipts for that
// rule" is far preferable to a typo'd pattern crashing the daemon.
func ShouldReceipt(cfg config.ReceiptsConfig, toolName, detail string) bool {
	if !cfg.Enabled {
		return false
	}
	target := toolName + " " + detail

	for _, pat := range cfg.DenyPatterns {
		if matchPattern(pat, target) {
			return false
		}
	}
	for _, pat := range cfg.AllowPatterns {
		if matchPattern(pat, target) {
			return true
		}
	}
	return false
}

// matchPattern evaluates a shell-glob-style pattern (`*` = any run of
// characters, `?` = any single character, `[set]`/`[^set]`/`[!set]` a
// character class) against target.
//
// This is deliberately NOT Go stdlib path.Match/filepath.Match despite both
// being an obvious first reach for "no new dependency": their `*` refuses to
// cross a `/` (path-separator-aware glob semantics — correct for matching
// one path segment, wrong here). target is "<tool_name> <detail>", and
// detail is very often a file path or a flag containing one — MADR 0077
// D10's own worked example is literally "bash rm -rf ./build". Verified
// directly: path.Match("*rm -rf*", "bash rm -rf ./build") returns
// (false, nil) — no error, just a silent non-match once anything after
// "rm -rf" contains a slash, which is the common case here, not an edge
// case. An operator's receipt-triggering pattern would silently never fire.
// Fix: translate the glob to a regexp (still Go stdlib, still no new
// dependency) with `*` mapped to `.*`, so it matches across `/` the way an
// operator typing a shell-style glob actually expects.
func matchPattern(pat, target string) bool {
	re, err := compileGlob(pat)
	if err != nil {
		slog.Warn("receipts: malformed pattern, treating as non-match",
			slog.String("pattern", pat), slog.String("err", err.Error()))
		return false
	}
	return re.MatchString(target)
}

var (
	globCacheMu sync.Mutex
	globCache   = make(map[string]*regexp.Regexp)
)

// compileGlob translates pat into an anchored regexp and caches the result —
// patterns come from config, loaded once and evaluated on every permission
// decision, so compiling on every call would be wasted work on a hot path.
func compileGlob(pat string) (*regexp.Regexp, error) {
	globCacheMu.Lock()
	defer globCacheMu.Unlock()
	if re, ok := globCache[pat]; ok {
		return re, nil
	}

	src, err := globToRegexpSource(pat)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, err
	}
	globCache[pat] = re
	return re, nil
}

// globToRegexpSource translates a shell-style glob into an anchored regexp
// source string. An unterminated `[...]` character class is the one
// malformed-pattern case this reports as an error (mirroring path.Match's
// ErrBadPattern for the same syntax mistake).
func globToRegexpSource(pat string) (string, error) {
	runes := []rune(pat)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i + 1
			neg := false
			if j < len(runes) && (runes[j] == '^' || runes[j] == '!') {
				neg = true
				j++
			}
			start := j
			// A ']' immediately after '[' or the negation marker is a
			// literal member of the class, not its terminator.
			if j < len(runes) && runes[j] == ']' {
				j++
			}
			for j < len(runes) && runes[j] != ']' {
				j++
			}
			if j >= len(runes) {
				return "", fmt.Errorf("unterminated character class in pattern %q", pat)
			}
			class := string(runes[start:j])
			b.WriteString("[")
			if neg {
				b.WriteString("^")
			}
			b.WriteString(strings.ReplaceAll(class, `\`, `\\`))
			b.WriteString("]")
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return b.String(), nil
}
