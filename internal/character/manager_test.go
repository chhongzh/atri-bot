package character

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestLocalProviderAndSystemTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dev.chhongzh.atri.yaml")
	if err := os.WriteFile(path, []byte("name: ATRI\npersonality: kind\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	characters, err := NewLocalProvider("local", root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != 1 || characters[0].ID != "dev.chhongzh.atri" {
		t.Fatalf("loaded characters = %#v", characters)
	}
	manager := New(nil, zap.NewNop(), Config{})
	manager.characters = map[string]*Character{characters[0].ID: characters[0]}
	prompt, err := manager.RenderSystemPrompt(context.Background(), characters[0].ID, "tester", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"dev.chhongzh.atri", "tester", "personality: kind", "instant-message conversation", "Two consecutive newline characters"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt %q does not contain %q", prompt, expected)
		}
	}
}
