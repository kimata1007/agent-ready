package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePreservesExistingInstructionsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexFile := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(codexFile), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(codexFile, []byte("# Existing guidance\n"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	manager := Manager{Home: home, AgentReadyRoot: filepath.Join(home, ".agent-ready")}
	if err := manager.Ensure(); err != nil {
		t.Fatalf("Ensure(first) error = %v", err)
	}
	if err := manager.Ensure(); err != nil {
		t.Fatalf("Ensure(second) error = %v", err)
	}
	for _, path := range []string{
		codexFile,
		filepath.Join(home, ".claude", "CLAUDE.md"),
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", path, err)
		}
		if strings.Count(string(content), startMarker) != 1 {
			t.Fatalf("managed block count in %s = %d", path, strings.Count(string(content), startMarker))
		}
	}
	content, _ := os.ReadFile(codexFile)
	if !strings.Contains(string(content), "# Existing guidance") {
		t.Fatalf("existing guidance was removed: %s", content)
	}
	info, err := os.Stat(codexFile)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestEnsureUsesEffectiveCodexOverride(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	if err := os.MkdirAll(filepath.Dir(override), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(override, []byte("# Override\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	manager := Manager{Home: home, AgentReadyRoot: filepath.Join(home, ".agent-ready")}
	if err := manager.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	content, err := os.ReadFile(override)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), startMarker) {
		t.Fatalf("override does not contain managed block: %s", content)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md unexpectedly created: %v", err)
	}
}
