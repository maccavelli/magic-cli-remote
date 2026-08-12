package goose

import (
	"sort"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// upstreamDef is one vendor goose can be pointed at.
//
// SecretKey is the name goose reads the credential under — the same string in
// its secret store and in the environment (`TOGETHER_API_KEY`). An empty
// SecretKey means the vendor is not key-authenticated at all: the subscription
// and CLI-backed providers (ChatGPT Codex, Gemini OAuth, Claude Code, GitHub
// Copilot, …) are configured by running that vendor's own login on the host,
// and no key exists for the phone to send.
type upstreamDef struct {
	ID        string
	Label     string
	SecretKey string
}

// gooseUpstreams is goose's provider registry, transcribed from the vendor's
// own metadata (MADR 0074 D16, D18).
//
// Why a table and not a live read: goose has no non-interactive way to list
// providers. `goose configure` takes no flags at all, the ACP surface carries
// no provider catalog, and the declarative definitions are compiled into the
// binary with include_dir!. So unlike OpenCode and Kilo — whose engines answer
// GET /provider — goose's catalog can only be pinned, and it is pinned here.
//
// Source: goose 1.46.0 checkout (crates/goose-providers/src/declarative/
// definitions/*.json for the data-defined vendors, ProviderMetadata +
// ConfigKey for the coded ones, canonical/catalog.rs for the ids real
// config.yaml files use). Installed CLI at the time of writing: 1.45.0.
//
// Drift is expected and handled rather than prevented: a provider goose knows
// about but this table does not still shows up when it is configured, because
// AuthStatus reports whatever config.yaml holds regardless of this list.
var gooseUpstreams = []upstreamDef{
	{ID: "alibaba", Label: "Alibaba (Qwen)", SecretKey: "DASHSCOPE_API_KEY"},
	{ID: "amp-acp", Label: "Amp", SecretKey: ""},
	{ID: "anthropic", Label: "Anthropic", SecretKey: "ANTHROPIC_API_KEY"},
	{ID: "atomic_chat", Label: "Atomic Chat", SecretKey: ""},
	{ID: "avian", Label: "Avian", SecretKey: "AVIAN_API_KEY"},
	{ID: "aws_bedrock", Label: "Amazon Bedrock", SecretKey: "AWS_BEARER_TOKEN_BEDROCK"},
	{ID: "azure_foundry", Label: "Azure AI Foundry", SecretKey: "AZURE_FOUNDRY_API_KEY"},
	{ID: "azure_openai", Label: "Azure OpenAI", SecretKey: "AZURE_OPENAI_API_KEY"},
	{ID: "celeris", Label: "Celeris", SecretKey: "CELERIS_API_KEY"},
	{ID: "cerebras", Label: "Cerebras", SecretKey: "CEREBRAS_API_KEY"},
	{ID: "chatgpt_codex", Label: "ChatGPT Codex", SecretKey: ""},
	{ID: "claude-acp", Label: "Claude Code", SecretKey: ""},
	{ID: "claude-code", Label: "Claude Code CLI", SecretKey: ""},
	{ID: "codex", Label: "OpenAI Codex CLI", SecretKey: ""},
	{ID: "codex-acp", Label: "Codex CLI", SecretKey: ""},
	{ID: "copilot-acp", Label: "GitHub Copilot CLI (ACP)", SecretKey: ""},
	{ID: "cursor-agent", Label: "Cursor Agent", SecretKey: ""},
	{ID: "custom_deepseek", Label: "DeepSeek", SecretKey: "DEEPSEEK_API_KEY"},
	{ID: "custom_tensorix", Label: "Tensorix", SecretKey: "TENSORIX_API_KEY"},
	{ID: "databricks", Label: "Databricks", SecretKey: "DATABRICKS_TOKEN"},
	{ID: "databricks_v2", Label: "Databricks AI Gateway", SecretKey: "DATABRICKS_TOKEN"},
	{ID: "empiriolabs", Label: "EmpirioLabs AI", SecretKey: "EMPIRIOLABS_API_KEY"},
	{ID: "fireworks-ai", Label: "Fireworks AI", SecretKey: "FIREWORKS_API_KEY"},
	{ID: "friendli", Label: "Friendli AI", SecretKey: "FRIENDLI_API_KEY"},
	{ID: "futurmix", Label: "FuturMix", SecretKey: "FUTURMIX_API_KEY"},
	{ID: "gcp_vertex_ai", Label: "GCP Vertex AI", SecretKey: ""},
	{ID: "gemini-cli", Label: "Gemini CLI", SecretKey: ""},
	{ID: "gemini_oauth", Label: "Gemini", SecretKey: ""},
	{ID: "github_copilot", Label: "GitHub Copilot", SecretKey: ""},
	{ID: "google", Label: "Google Gemini (API Key)", SecretKey: "GOOGLE_API_KEY"},
	{ID: "goose", Label: "goose", SecretKey: ""},
	{ID: "groq", Label: "Groq (d)", SecretKey: "GROQ_API_KEY"},
	{ID: "huggingface", Label: "Hugging Face", SecretKey: ""},
	{ID: "iflytek", Label: "iFlytek Spark", SecretKey: "SPARK_API_PASSWORD"},
	{ID: "iflytek_astron", Label: "iFlytek Astron MaaS", SecretKey: "ASTRON_API_KEY"},
	{ID: "inception", Label: "Inception", SecretKey: "INCEPTION_API_KEY"},
	{ID: "kimi_code", Label: "Kimi Code", SecretKey: ""},
	{ID: "litellm", Label: "LiteLLM", SecretKey: "LITELLM_API_KEY"},
	{ID: "llama_swap", Label: "Llama Swap", SecretKey: "LLAMA_SWAP_API_KEY"},
	{ID: "lmstudio", Label: "LM Studio", SecretKey: "LMSTUDIO_API_KEY"},
	{ID: "meta", Label: "Meta", SecretKey: "META_MODEL_API_KEY"},
	{ID: "minimax", Label: "MiniMax", SecretKey: "MINIMAX_API_KEY"},
	{ID: "mistral", Label: "Mistral AI", SecretKey: "MISTRAL_API_KEY"},
	{ID: "moonshot", Label: "Moonshot", SecretKey: "MOONSHOT_API_KEY"},
	{ID: "nano-gpt", Label: "NanoGPT", SecretKey: ""},
	{ID: "nearai", Label: "NEAR AI Cloud", SecretKey: "NEARAI_API_KEY"},
	{ID: "novita", Label: "Novita AI", SecretKey: "NOVITA_API_KEY"},
	{ID: "nvidia", Label: "NVIDIA", SecretKey: "NVIDIA_API_KEY"},
	{ID: "ollama", Label: "Ollama", SecretKey: ""},
	{ID: "ollama_cloud", Label: "Ollama Cloud", SecretKey: "OLLAMA_CLOUD_API_KEY"},
	{ID: "omlx", Label: "oMLX", SecretKey: "OMLX_API_KEY"},
	{ID: "openai", Label: "OpenAI", SecretKey: "OPENAI_API_KEY"},
	{ID: "opencode_go", Label: "OpenCode Go", SecretKey: "OPENCODE_API_KEY"},
	{ID: "openrouter", Label: "OpenRouter", SecretKey: "OPENROUTER_API_KEY"},
	{ID: "orcarouter", Label: "OrcaRouter", SecretKey: "ORCAROUTER_API_KEY"},
	{ID: "ovhcloud", Label: "OVHcloud", SecretKey: "OVHCLOUD_API_KEY"},
	{ID: "perplexity", Label: "Perplexity", SecretKey: "PERPLEXITY_API_KEY"},
	{ID: "pi-acp", Label: "Pi", SecretKey: ""},
	{ID: "routstr", Label: "Routstr", SecretKey: "ROUTSTR_API_KEY"},
	{ID: "sagemaker_tgi", Label: "Amazon SageMaker TGI", SecretKey: ""},
	{ID: "sakana", Label: "Sakana AI", SecretKey: "SAKANA_API_KEY"},
	{ID: "saladcloud", Label: "SaladCloud AI Gateway", SecretKey: "SALAD_CLOUD_API_KEY"},
	{ID: "scaleway", Label: "Scaleway", SecretKey: "SCW_SECRET_KEY"},
	{ID: "snowflake", Label: "Snowflake", SecretKey: "SNOWFLAKE_TOKEN"},
	{ID: "tanzu_ai", Label: "VMware Tanzu Platform", SecretKey: "TANZU_AI_API_KEY"},
	{ID: "tetrate", Label: "Tetrate Agent Router Service", SecretKey: "TETRATE_API_KEY"},
	{ID: "together", Label: "Together AI", SecretKey: "TOGETHER_API_KEY"},
	{ID: "venice", Label: "Venice.ai", SecretKey: "VENICE_API_KEY"},
	{ID: "vercel_ai_gateway", Label: "Vercel AI Gateway", SecretKey: "AI_GATEWAY_API_KEY"},
	{ID: "xai", Label: "xAI", SecretKey: "XAI_API_KEY"},
	{ID: "xai_oauth", Label: "xAI (SuperGrok Subscription)", SecretKey: ""},
	{ID: "zai", Label: "Z.AI", SecretKey: "ZHIPU_API_KEY"},
	{ID: "zhipu", Label: "Zhipu AI", SecretKey: "ZHIPU_API_KEY"},
}

// GooseCatalogVersion is the goose release this table was transcribed from. It
// rides along in nothing user-facing; it exists so a reader can tell how old
// the pin is without going to git.
const GooseCatalogVersion = "1.46.0"

// catalogByID indexes gooseUpstreams for label lookups from AuthStatus.
var catalogByID = func() map[string]upstreamDef {
	m := make(map[string]upstreamDef, len(gooseUpstreams))
	for _, u := range gooseUpstreams {
		m[u.ID] = u
	}
	return m
}()

// upstreamLabel is the human name for an id, falling back to the id itself for
// a provider newer than this table.
func upstreamLabel(id string) string {
	if u, ok := catalogByID[id]; ok && u.Label != "" {
		return u.Label
	}
	return id
}

// catalogMethods is what the phone may offer for one vendor.
func catalogMethods(u upstreamDef) []provider.AuthMethod {
	if u.SecretKey != "" {
		return []provider.AuthMethod{{
			ID:    u.ID + ":api",
			Type:  provider.AuthMethodAPIKey,
			Label: "API key (" + u.SecretKey + ")",
		}}
	}
	// No key to paste: goose gets these from another CLI's own session or an
	// OAuth flow it runs itself. Typing them as browser OAuth makes the phone
	// render them disabled with "requires host access", which is exactly the
	// truth, instead of offering a key field that cannot work.
	return []provider.AuthMethod{{
		ID:    u.ID + ":host",
		Type:  provider.AuthMethodOAuthBrowser,
		Label: "Sign in on the host (goose configure)",
	}}
}

// authCatalog implements [provider.AuthCataloger] for goose (MADR 0074 D16).
func authCatalog(configured map[string]struct{}) provider.AuthCatalog {
	out := make([]provider.UpstreamAuth, 0, len(gooseUpstreams))
	for _, u := range gooseUpstreams {
		st := provider.AuthMissing
		if _, ok := configured[u.ID]; ok {
			st = provider.AuthConfigured
		}
		out = append(out, provider.UpstreamAuth{
			ID:      u.ID,
			Label:   u.Label,
			Status:  st,
			Methods: catalogMethods(u),
		})
	}
	// A provider configured on this host but absent from the pinned table is
	// still real; showing it beats pretending goose cannot use it.
	for id := range configured {
		if _, known := catalogByID[id]; known {
			continue
		}
		out = append(out, provider.UpstreamAuth{
			ID:     id,
			Label:  id,
			Status: provider.AuthConfigured,
			Methods: []provider.AuthMethod{{
				ID:    id + ":api",
				Type:  provider.AuthMethodAPIKey,
				Label: "API key",
			}},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return provider.AuthCatalog{Upstreams: out, Source: provider.AuthCatalogSourceStatic}
}

// secretKeyFor reports the secret name a vendor's key is stored under, and
// whether the vendor takes a key at all.
func secretKeyFor(id string) (string, bool) {
	u, ok := catalogByID[strings.TrimSpace(id)]
	if !ok || u.SecretKey == "" {
		return "", false
	}
	return u.SecretKey, true
}
