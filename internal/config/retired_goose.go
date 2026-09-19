package config

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/spf13/viper"
)

// retiredGooseCode is the Diagnostic code for a providers.goose setting left
// over from before MADR 0160 removed the Goose provider.
const retiredGooseCode = "retired_provider_goose"

// retiredGooseEnvPrefix is the environment prefix that used to reach the
// providers.goose keys through AutomaticEnv.
const retiredGooseEnvPrefix = "MCREMOTE_PROVIDERS_GOOSE_"

// noteRetiredGoose reports, without failing the load, a providers.goose
// block or MCREMOTE_PROVIDERS_GOOSE_* variable (MADR 0160 D3). Refusing
// would stop every host setup-service provisioned before 0160, because the
// seed config carried the block (F16).
//
// A bare `goose:` key with no value is invisible here: viper treats it as
// absent, and it configures nothing (F17).
func noteRetiredGoose(v *viper.Viper, environ []string, cfg *Config) {
	var sources []string
	if v.InConfig("providers.goose") {
		sources = append(sources, "providers.goose in "+cfg.ConfigFile)
	}
	var envNames []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, retiredGooseEnvPrefix) {
			envNames = append(envNames, name)
		}
	}
	sort.Strings(envNames)
	sources = append(sources, envNames...)
	if len(sources) == 0 {
		return
	}
	joined := strings.Join(sources, ", ")
	msg := joined + " ignored: the Goose provider was removed (MADR 0160); delete the setting"
	slog.Default().Warn("retired goose settings ignored", slog.String("sources", joined))
	cfg.Diagnostics = append(cfg.Diagnostics, appdirs.Diagnostic{Code: retiredGooseCode, Message: msg})
}
