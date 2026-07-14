// Package agent is ihme's embedded-agent adapter: it wires the
// agentkit kernel to the application service. BYOK configuration,
// consent policy, tools, and rendering all live here — the kernel
// stays generic.
package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the BYOK model configuration. Resolution order per
// field: agent.json, then environment (OPENAI_MODEL /
// OPENAI_BASE_URL / OPENAI_API_KEY). The key itself is never stored
// in agent.json — only the name of the env var holding it.
type Config struct {
	Model     string `json:"model"`
	BaseURL   string `json:"baseUrl"`
	APIKeyEnv string `json:"apiKeyEnv"`

	// API selects the wire protocol: "completions" (default; the
	// OpenAI-compatible lingua franca) or "responses" (required by
	// reasoning models for function tools).
	API string `json:"api"`
	// Effort sets reasoning effort for the responses API
	// ("low"/"medium"/"high"); empty omits the parameter.
	Effort string `json:"effort"`
}

func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ihme")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ihme")
}

// LoadConfig reads ~/.config/ihme/.env (existing env always wins),
// then agent.json, then env fallbacks, and resolves the API key.
func LoadConfig() (Config, string, error) {
	loadEnvFile(filepath.Join(configDir(), ".env"))

	var cfg Config
	if raw, err := os.ReadFile(filepath.Join(configDir(), "agent.json")); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, "", fmt.Errorf("parsing %s: %w", filepath.Join(configDir(), "agent.json"), err)
		}
	}
	if cfg.Model == "" {
		cfg.Model = os.Getenv("OPENAI_MODEL")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = "OPENAI_API_KEY"
	}
	if cfg.API == "" {
		cfg.API = os.Getenv("OPENAI_API")
	}
	if cfg.API == "" {
		cfg.API = "completions"
	}
	if cfg.API != "completions" && cfg.API != "responses" {
		return Config{}, "", fmt.Errorf("invalid api %q in agent config — use \"completions\" or \"responses\"", cfg.API)
	}

	key := os.Getenv(cfg.APIKeyEnv)
	switch {
	case cfg.Model == "" || cfg.BaseURL == "":
		return Config{}, "", fmt.Errorf(
			"agent not configured — set model and baseUrl in %s/agent.json or OPENAI_MODEL/OPENAI_BASE_URL",
			configDir())
	case key == "":
		return Config{}, "", fmt.Errorf("no API key in $%s — put it in %s/.env or export it", cfg.APIKeyEnv, configDir())
	}
	return cfg, key, nil
}

// loadEnvFile sets KEY=VALUE lines that are not already in the
// environment. Quotes are stripped; malformed lines are skipped.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, val)
	}
}
