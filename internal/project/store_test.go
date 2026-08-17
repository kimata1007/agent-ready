package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCreatesGlobalProject(t *testing.T) {
	t.Parallel()
	store := Store{Root: filepath.Join(t.TempDir(), ".agent-ready")}
	created := time.Date(2026, 8, 17, 1, 2, 3, 0, time.UTC)
	err := store.Create(Project{
		Name:      "payments",
		Analyzer:  AnalyzerConfig{Provider: "codex"},
		CreatedAt: created,
		UpdatedAt: created,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	loaded, err := store.LoadProject("payments")
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}
	if loaded.Name != "payments" || loaded.Analyzer.Provider != "codex" {
		t.Fatalf("LoadProject() = %#v", loaded)
	}
	sources, err := store.LoadSources("payments")
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	if sources.Project != "payments" || len(sources.Sources) != 0 {
		t.Fatalf("LoadSources() = %#v", sources)
	}
	info, err := os.Stat(filepath.Join(store.Root, "payments", projectFile))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("project file mode = %o", info.Mode().Perm())
	}
}

func TestStoreObjectsAreContentAddressed(t *testing.T) {
	t.Parallel()
	store := Store{Root: t.TempDir()}
	if err := store.Create(Project{Name: "docs", Analyzer: AnalyzerConfig{Provider: "claude"}}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	digest, err := store.PutObject("docs", []byte("hello"))
	if err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	content, err := store.ReadObject("docs", digest)
	if err != nil {
		t.Fatalf("ReadObject() error = %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("ReadObject() = %q", content)
	}
}

func TestValidateNameRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "..", "../private", "a/b"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) succeeded", name)
		}
	}
}

func TestRebuildIndexPublishesContextLocations(t *testing.T) {
	t.Parallel()
	store := Store{Root: t.TempDir()}
	if err := store.Create(Project{Name: "docs", Analyzer: AnalyzerConfig{Provider: "codex"}}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sources, err := store.LoadSources("docs")
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	sources.Sources = []Source{{Kind: "directory", Locator: "/workspace/docs"}}
	if err := store.SaveSources(sources); err != nil {
		t.Fatalf("SaveSources() error = %v", err)
	}
	if err := store.RebuildIndex(time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RebuildIndex() error = %v", err)
	}
	var index Index
	if err := readJSON(filepath.Join(store.Root, "index.json"), &index); err != nil {
		t.Fatalf("readJSON(index) error = %v", err)
	}
	if len(index.Projects) != 1 || index.Projects[0].Name != "docs" ||
		index.Projects[0].ContextPath != filepath.Join(store.Root, "docs", "context.md") {
		t.Fatalf("Index = %#v", index)
	}
}
