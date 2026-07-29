package agent

import "testing"

// clearBYOKEnv isolates LoadConfig from the developer's real
// environment and config directory.
func clearBYOKEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, v := range []string{
		"OPENAI_MODEL", "OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_API",
		"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY",
	} {
		t.Setenv(v, "")
	}
}

func TestClaudeModelIsACompleteConfiguration(t *testing.T) {
	// ANTHROPIC_MODEL + ANTHROPIC_API_KEY alone must resolve: base
	// URL defaults to Anthropic's endpoint, key env follows the host.
	clearBYOKEnv(t)
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-5")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	cfg, key, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://api.anthropic.com" || cfg.APIKeyEnv != "ANTHROPIC_API_KEY" || key != "sk-ant-test" {
		t.Fatalf("cfg = %+v key = %q", cfg, key)
	}
}

func TestGatewayClaudeKeepsOpenAIKeyConvention(t *testing.T) {
	// A claude model behind an OpenAI-protocol gateway: the base URL
	// is not Anthropic's, so the key default stays OPENAI_API_KEY.
	clearBYOKEnv(t)
	t.Setenv("OPENAI_MODEL", "claude-opus-5")
	t.Setenv("OPENAI_BASE_URL", "https://gw.example.com/v1")
	t.Setenv("OPENAI_API_KEY", "sk-gw-test")

	cfg, key, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKeyEnv != "OPENAI_API_KEY" || key != "sk-gw-test" || cfg.BaseURL != "https://gw.example.com/v1" {
		t.Fatalf("cfg = %+v key = %q", cfg, key)
	}
}

func TestNonClaudeModelStillRequiresBaseURL(t *testing.T) {
	clearBYOKEnv(t)
	t.Setenv("OPENAI_MODEL", "gpt-5.6")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if _, _, err := LoadConfig(); err == nil {
		t.Fatal("want not-configured error without a base URL")
	}
}
