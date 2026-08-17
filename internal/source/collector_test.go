package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kimata1007/agent-ready/internal/project"
)

func newTestCollector(t *testing.T) Collector {
	t.Helper()
	store := project.Store{Root: t.TempDir()}
	if err := store.Create(project.Project{Name: "test", Analyzer: project.AnalyzerConfig{Provider: "codex"}}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return Collector{
		Store: store,
		Now: func() time.Time {
			return time.Date(2026, 8, 17, 1, 2, 3, 0, time.UTC)
		},
	}
}

func TestCollectsPastedMarkdown(t *testing.T) {
	t.Parallel()
	collector := newTestCollector(t)
	collected, err := collector.Collect(context.Background(), "test", Input{
		Value:   "-",
		Name:    "decisions",
		Content: []byte("# Decisions\n\nKeep the API stable.\n"),
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	defer collected.Cleanup()
	if collected.Source.Kind != "text" || collected.Source.Object == "" {
		t.Fatalf("Source = %#v", collected.Source)
	}
	content, err := os.ReadFile(filepath.Join(collected.Workspace, "input.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "# Decisions\n\nKeep the API stable.\n" {
		t.Fatalf("workspace content = %q", content)
	}
}

func TestCollectsWebDocument(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("# API\n"))
	}))
	defer server.Close()
	collector := newTestCollector(t)
	collected, err := collector.Collect(context.Background(), "test", Input{Value: server.URL + "/guide.md"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	defer collected.Cleanup()
	if collected.Source.Kind != "web" || collected.Source.Name != "guide" {
		t.Fatalf("Source = %#v", collected.Source)
	}
}

func TestDirectoryDigestChangesWithContent(t *testing.T) {
	t.Parallel()
	collector := newTestCollector(t)
	directory := t.TempDir()
	file := filepath.Join(directory, "README.md")
	if err := os.WriteFile(file, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	first, err := collector.Collect(context.Background(), "test", Input{Value: directory})
	if err != nil {
		t.Fatalf("Collect(first) error = %v", err)
	}
	if err := os.WriteFile(file, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	second, err := collector.Collect(context.Background(), "test", Input{Value: directory})
	if err != nil {
		t.Fatalf("Collect(second) error = %v", err)
	}
	if first.Source.Digest == second.Source.Digest {
		t.Fatal("directory digest did not change")
	}
	if first.Source.ID != second.Source.ID {
		t.Fatal("source ID changed with content")
	}
}

func TestRejectsCredentialBearingURL(t *testing.T) {
	t.Parallel()
	collector := newTestCollector(t)
	_, err := collector.Collect(
		context.Background(),
		"test",
		Input{Value: "https://example.test/document?access_token=redacted"},
	)
	if err == nil {
		t.Fatal("Collect() succeeded")
	}
}

func TestAcceptsSSHGitUsernameWithoutPassword(t *testing.T) {
	t.Parallel()
	locator := "ssh://git" + "@" + "example.test/team/service.git"
	remote, kind, ok, err := classifyRemote(locator)
	if err != nil {
		t.Fatalf("classifyRemote() error = %v", err)
	}
	if !ok || kind != "git" || remote != locator {
		t.Fatalf("classifyRemote() = %q, %q, %t", remote, kind, ok)
	}
}
