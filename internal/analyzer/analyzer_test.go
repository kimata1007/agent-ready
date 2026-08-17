package analyzer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kimata1007/agent-ready/internal/project"
)

type fakeRunner struct {
	result  CommandResult
	name    string
	args    []string
	dir     string
	invoked int
}

func (runner *fakeRunner) Run(
	_ context.Context,
	directory, name string,
	arguments []string,
	_ []byte,
) (CommandResult, error) {
	runner.invoked++
	runner.name = name
	runner.args = append([]string(nil), arguments...)
	runner.dir = directory
	return runner.result, nil
}

func TestCodexAnalyzerUsesReadOnlyStructuredExecution(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{result: CommandResult{Stdout: []byte(`{
  "summary":"API implementation", "purpose":"Handles requests",
  "keyConcepts":["routing"],
  "locations":[{"path":"cmd/server/main.go","description":"entry point"}],
  "usage":["Read before changing routes"], "warnings":[]
}`)}}
	client, err := New(project.AnalyzerConfig{Provider: "codex"}, runner)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysis, err := client.Analyze(context.Background(), project.Source{
		ID: "api-123", Name: "api", Kind: "git", Locator: "https://example.test/api.git",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if analysis.SourceID != "api-123" || analysis.Locations[0].Path != "cmd/server/main.go" {
		t.Fatalf("Analyze() = %#v", analysis)
	}
	joined := strings.Join(runner.args, " ")
	if runner.name != "codex" || !strings.Contains(joined, "--sandbox read-only") ||
		!strings.Contains(joined, "--output-schema") || !strings.Contains(joined, "--ephemeral") {
		t.Fatalf("codex invocation = %s %s", runner.name, joined)
	}
}

func TestClaudeAnalyzerRestrictsToolsAndReadsStructuredOutput(t *testing.T) {
	t.Parallel()
	structured := json.RawMessage(`{
  "summary":"Design guide", "purpose":"Defines constraints",
  "keyConcepts":[], "locations":[], "usage":[], "warnings":[]
}`)
	envelope, err := json.Marshal(map[string]any{
		"type":              "result",
		"structured_output": structured,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	runner := &fakeRunner{result: CommandResult{Stdout: envelope}}
	client, err := New(project.AnalyzerConfig{Provider: "claude"}, runner)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.Analyze(context.Background(), project.Source{
		ID: "guide-123", Name: "guide", Kind: "file", Locator: "/docs/guide.md",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	joined := strings.Join(runner.args, " ")
	if runner.name != "claude" || !strings.Contains(joined, "--tools Read,Glob,Grep") ||
		!strings.Contains(joined, "--json-schema") || !strings.Contains(joined, "--no-session-persistence") {
		t.Fatalf("claude invocation = %s %s", runner.name, joined)
	}
}

func TestSynthesizeUsesAllAnalyses(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{result: CommandResult{Stdout: []byte(`{
  "overview":"A service and its guide", "keyConcepts":["API"],
  "workflows":["change then test"], "sourceGuidance":["Use the guide for constraints"]
}`)}}
	client, err := New(project.AnalyzerConfig{Provider: "codex"}, runner)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := client.Synthesize(context.Background(), project.Project{Name: "demo"}, []project.SourceAnalysis{
		{SourceID: "api", Summary: "API", Purpose: "Serve"},
		{SourceID: "guide", Summary: "Guide", Purpose: "Constrain"},
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if result.Overview != "A service and its guide" || runner.invoked != 1 {
		t.Fatalf("Synthesize() = %#v, calls = %d", result, runner.invoked)
	}
}
