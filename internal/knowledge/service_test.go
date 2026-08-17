package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kimata1007/agent-ready/internal/analyzer"
	"github.com/kimata1007/agent-ready/internal/project"
	"github.com/kimata1007/agent-ready/internal/source"
)

type fakeAnalyzer struct {
	analyzed    []string
	synthesized int
}

type recordedProgress struct {
	task ProgressTask
	err  error
}

type fakeProgressReporter struct {
	records []*recordedProgress
}

func (reporter *fakeProgressReporter) Start(task ProgressTask) func(error) {
	record := &recordedProgress{task: task}
	reporter.records = append(reporter.records, record)
	return func(err error) {
		record.err = err
	}
}

func (fake *fakeAnalyzer) Analyze(
	_ context.Context,
	saved project.Source,
	_ string,
) (project.SourceAnalysis, error) {
	fake.analyzed = append(fake.analyzed, saved.ID)
	return project.SourceAnalysis{
		SchemaVersion: project.SchemaVersion,
		SourceID:      saved.ID,
		Summary:       "Summary of " + saved.Name,
		Purpose:       "Reference " + saved.Name,
		KeyConcepts:   []string{"concept"},
		Locations:     []project.Location{{Path: "README.md", Description: "overview"}},
		Usage:         []string{"before changing the project"},
		Warnings:      []string{},
	}, nil
}

func (fake *fakeAnalyzer) Synthesize(
	_ context.Context,
	_ project.Project,
	_ []project.SourceAnalysis,
) (project.ProjectAnalysis, error) {
	fake.synthesized++
	return project.ProjectAnalysis{
		Overview:       "Project overview",
		KeyConcepts:    []string{"concept"},
		Workflows:      []string{"read, change, test"},
		SourceGuidance: []string{"Use registered sources as evidence"},
	}, nil
}

func newTestService(t *testing.T) (Service, project.Store, *fakeAnalyzer) {
	t.Helper()
	store := project.Store{Root: filepath.Join(t.TempDir(), ".agent-ready")}
	fake := &fakeAnalyzer{}
	now := time.Date(2026, 8, 17, 1, 2, 3, 0, time.UTC)
	service := Service{
		Store: store,
		NewAnalyzer: func(project.AnalyzerConfig) (analyzer.Analyzer, error) {
			return fake, nil
		},
		Now: func() time.Time { return now },
	}
	return service, store, fake
}

func TestInitBuildsCatalogFromMultipleSources(t *testing.T) {
	t.Parallel()
	service, store, fake := newTestService(t)
	document := filepath.Join(t.TempDir(), "architecture.md")
	if err := os.WriteFile(document, []byte("# Architecture\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, err := service.Init(context.Background(), InitOptions{
		Project:  "payments",
		Analyzer: project.AnalyzerConfig{Provider: "codex"},
		Sources: []source.Input{
			{Value: document},
			{Value: "-", Name: "decisions", Content: []byte("# Decisions\n")},
		},
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.SourceCount != 2 || len(fake.analyzed) != 2 || fake.synthesized != 1 {
		t.Fatalf("Init() = %#v, analyzed = %v, synthesized = %d", result, fake.analyzed, fake.synthesized)
	}
	catalog, err := store.LoadCatalog("payments")
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	if len(catalog.Entries) != 2 || catalog.Overview != "Project overview" {
		t.Fatalf("Catalog = %#v", catalog)
	}
	contextContent, err := os.ReadFile(filepath.Join(store.Root, "payments", "context.md"))
	if err != nil {
		t.Fatalf("ReadFile(context.md) error = %v", err)
	}
	if !strings.Contains(string(contextContent), "Source routing") {
		t.Fatalf("context.md = %s", contextContent)
	}
}

func TestInitReportsLongRunningStages(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService(t)
	reporter := &fakeProgressReporter{}
	service.Progress = reporter
	document := filepath.Join(t.TempDir(), "architecture.md")
	if err := os.WriteFile(document, []byte("# Architecture\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := service.Init(context.Background(), InitOptions{
		Project:  "payments",
		Analyzer: project.AnalyzerConfig{Provider: "codex"},
		Sources: []source.Input{
			{Value: document},
			{Value: "-", Name: "decisions", Content: []byte("# Decisions\n")},
		},
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	wantPhases := []ProgressPhase{
		ProgressCollect,
		ProgressAnalyze,
		ProgressCollect,
		ProgressAnalyze,
		ProgressSynthesize,
	}
	if len(reporter.records) != len(wantPhases) {
		t.Fatalf("progress records = %d, want %d", len(reporter.records), len(wantPhases))
	}
	for index, want := range wantPhases {
		record := reporter.records[index]
		if record.task.Phase != want {
			t.Errorf("record %d phase = %q, want %q", index, record.task.Phase, want)
		}
		if record.err != nil {
			t.Errorf("record %d error = %v", index, record.err)
		}
	}
	if reporter.records[0].task.Current != 1 || reporter.records[0].task.Total != 2 {
		t.Errorf("first progress task = %#v", reporter.records[0].task)
	}
	if reporter.records[3].task.Provider != "codex" {
		t.Errorf("analyzer provider = %q", reporter.records[3].task.Provider)
	}
	if reporter.records[4].task.Source != "payments" || reporter.records[4].task.Total != 2 {
		t.Errorf("synthesis task = %#v", reporter.records[4].task)
	}
}

func TestPendingSourceNameDoesNotExposeURLQuery(t *testing.T) {
	t.Parallel()
	input := source.Input{Value: "https://example.test/docs/guide?tracking=opaque"}
	name := pendingSourceName(input)
	if name != "example.test/guide" {
		t.Fatalf("pendingSourceName() = %q", name)
	}
	for _, queryPart := range []string{"tracking", "opaque"} {
		if strings.Contains(name, queryPart) {
			t.Errorf("pending source name exposes %q: %q", queryPart, name)
		}
	}
}

func TestAddAndRefreshOnlyAnalyzeChanges(t *testing.T) {
	t.Parallel()
	service, _, fake := newTestService(t)
	document := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(document, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := service.Init(context.Background(), InitOptions{
		Project: "docs", Sources: []source.Input{{Value: document}},
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	fake.analyzed = nil
	fake.synthesized = 0
	unchanged, err := service.Refresh(context.Background(), "docs")
	if err != nil {
		t.Fatalf("Refresh(unchanged) error = %v", err)
	}
	if unchanged.CatalogUpdated || len(fake.analyzed) != 0 || fake.synthesized != 0 {
		t.Fatalf("Refresh(unchanged) = %#v, analyzed = %v", unchanged, fake.analyzed)
	}
	if err := os.WriteFile(document, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	changed, err := service.Refresh(context.Background(), "docs")
	if err != nil {
		t.Fatalf("Refresh(changed) error = %v", err)
	}
	if !changed.CatalogUpdated || len(changed.Changed) != 1 || len(fake.analyzed) != 1 || fake.synthesized != 1 {
		t.Fatalf("Refresh(changed) = %#v, analyzed = %v, synthesized = %d", changed, fake.analyzed, fake.synthesized)
	}
}

func TestAddAppendsSourceAndRebuildsCatalog(t *testing.T) {
	t.Parallel()
	service, store, fake := newTestService(t)
	first := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(first, []byte("guide"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := service.Init(context.Background(), InitOptions{
		Project: "docs", Sources: []source.Input{{Value: first}},
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	fake.analyzed = nil
	fake.synthesized = 0
	result, err := service.Add(context.Background(), AddOptions{
		Project: "docs",
		Sources: []source.Input{{
			Value: "-", Name: "notes", Content: []byte("# Notes\n"),
		}},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if result.SourceCount != 2 || len(result.Added) != 1 || len(fake.analyzed) != 1 || fake.synthesized != 1 {
		t.Fatalf("Add() = %#v, analyzed = %v, synthesized = %d", result, fake.analyzed, fake.synthesized)
	}
	catalog, err := store.LoadCatalog("docs")
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	if len(catalog.Entries) != 2 {
		t.Fatalf("catalog entries = %d", len(catalog.Entries))
	}
}

func TestAddRejectsSameSourceUnderDifferentName(t *testing.T) {
	t.Parallel()
	service, _, fake := newTestService(t)
	document := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(document, []byte("guide"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := service.Init(context.Background(), InitOptions{
		Project: "docs", Sources: []source.Input{{Value: document, Name: "guide"}},
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	fake.analyzed = nil
	_, err := service.Add(context.Background(), AddOptions{
		Project: "docs", Sources: []source.Input{{Value: document, Name: "renamed"}},
	})
	if err == nil {
		t.Fatal("Add() succeeded")
	}
	if len(fake.analyzed) != 0 {
		t.Fatalf("duplicate source was analyzed: %v", fake.analyzed)
	}
}
