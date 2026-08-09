package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetConfig(t *testing.T) {
	withWorkingDirectory(t, `
telegram:
  bot_token: test-token
atri_cwd: /tmp/atri
character_repository_url: https://example.com/chardefs.git
character_repository_branch: main
bot:
  max_rounds: 24
`)

	cfg, err := getConfig()
	if err != nil {
		t.Fatalf("getConfig() error = %v", err)
	}
	if cfg.botToken != "test-token" {
		t.Errorf("botToken = %q, want test-token", cfg.botToken)
	}
	if cfg.cwd != "/tmp/atri" {
		t.Errorf("cwd = %q, want /tmp/atri", cfg.cwd)
	}
	if cfg.characterRepoURL != "https://example.com/chardefs.git" {
		t.Errorf("characterRepoURL = %q", cfg.characterRepoURL)
	}
	if cfg.characterRepoBranch != "main" {
		t.Errorf("characterRepoBranch = %q, want main", cfg.characterRepoBranch)
	}
	if cfg.defaultMaxRounds != 24 {
		t.Errorf("defaultMaxRounds = %d, want 24", cfg.defaultMaxRounds)
	}
}

func TestGetConfigRequiresBotToken(t *testing.T) {
	withWorkingDirectory(t, "telegram: {}\n")

	_, err := getConfig()
	if err == nil || !strings.Contains(err.Error(), "telegram.bot_token") {
		t.Fatalf("getConfig() error = %v, want missing telegram.bot_token error", err)
	}
}

func TestGetConfigRejectsNonPositiveDefaultMaxRounds(t *testing.T) {
	withWorkingDirectory(t, "telegram:\n  bot_token: test-token\nbot:\n  max_rounds: 0\n")

	_, err := getConfig()
	if err == nil || !strings.Contains(err.Error(), "bot.max_rounds") {
		t.Fatalf("getConfig() error = %v, want invalid bot.max_rounds error", err)
	}
}

func withWorkingDirectory(t *testing.T, config string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}
