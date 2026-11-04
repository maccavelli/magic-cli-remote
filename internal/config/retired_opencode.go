package config

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/spf13/viper"
)

// retiredOpencodeCode is the Diagnostic code for a providers.opencode key of
// the OpenCode 1.x dialect, retired with it by 0214 PLAN P7.
const retiredOpencodeCode = "retired_opencode_1x_keys"

// retiredOpencodeKeys are the 1.x dialect's own keys under providers.opencode.
var retiredOpencodeKeys = []string{"pure", "session_tree", "stream_coalesce_ms"}

// noteRetiredOpencode reports, without failing the load, a 1.x dialect key
// left in providers.opencode or as its MCREMOTE_PROVIDERS_OPENCODE_* variable
// (0214 PLAN P7 Deviation 2). Refusing would stop every host setup-service
// provisioned before P7, because the seed config carried all three; the
// value is ignored, whatever it is, as noteRetiredGoose does (MADR 0160 D3).
func noteRetiredOpencode(v *viper.Viper, environ []string, cfg *Config) {
	var sources []string
	envNames := map[string]bool{}
	for _, key := range retiredOpencodeKeys {
		if v.InConfig("providers.opencode." + key) {
			sources = append(sources, "providers.opencode."+key+" in "+cfg.ConfigFile)
		}
		envNames["MCREMOTE_PROVIDERS_OPENCODE_"+strings.ToUpper(key)] = true
	}
	var envSet []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if envNames[name] {
			envSet = append(envSet, name)
		}
	}
	sort.Strings(envSet)
	sources = append(sources, envSet...)
	if len(sources) == 0 {
		return
	}
	joined := strings.Join(sources, ", ")
	msg := joined + " ignored: it configured the OpenCode 1.x dialect, retired by " +
		"0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md P7; delete the setting"
	slog.Default().Warn("retired opencode settings ignored", slog.String("sources", joined))
	cfg.Diagnostics = append(cfg.Diagnostics, appdirs.Diagnostic{Code: retiredOpencodeCode, Message: msg})
}
