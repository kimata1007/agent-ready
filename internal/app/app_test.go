package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kimata1007/agent-ready/internal/knowledge"
)

type fakeKnowledgeService struct {
	initOptions knowledge.InitOptions
	addOptions  knowledge.AddOptions
	refreshed   string
}

func (fake *fakeKnowledgeService) Init(
	_ context.Context,
	options knowledge.InitOptions,
) (knowledge.Result, error) {
	fake.initOptions = options
	return knowledge.Result{Project: options.Project, SourceCount: len(options.Sources), CatalogUpdated: true}, nil
}

func (fake *fakeKnowledgeService) Add(
	_ context.Context,
	options knowledge.AddOptions,
) (knowledge.Result, error) {
	fake.addOptions = options
	return knowledge.Result{Project: options.Project, SourceCount: len(options.Sources), CatalogUpdated: true}, nil
}

func (fake *fakeKnowledgeService) Refresh(
	_ context.Context,
	projectName string,
) (knowledge.Result, error) {
	fake.refreshed = projectName
	return knowledge.Result{Project: projectName, SourceCount: 2}, nil
}

func TestUsageListsOnlyKnowledgeCommands(t *testing.T) {
	t.Parallel()
	var output, errors bytes.Buffer
	application := New(strings.NewReader(""), &output, &errors)
	if code := application.Run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("Run() code = %d", code)
	}
	usage := output.String()
	for _, command := range []string{"init", "add", "refresh"} {
		if !strings.Contains(usage, "agent-ready "+command) {
			t.Fatalf("usage does not contain %q: %s", command, usage)
		}
	}
	for _, removed := range []string{"startup", "context create", "integration install"} {
		if strings.Contains(usage, removed) {
			t.Fatalf("usage contains removed command %q: %s", removed, usage)
		}
	}
}

func TestInitAcceptsMultipleSourcesAndPastedText(t *testing.T) {
	t.Parallel()
	var output, errors bytes.Buffer
	fake := &fakeKnowledgeService{}
	application := New(strings.NewReader("# Project notes\n"), &output, &errors)
	application.Service = fake
	application.Root = t.TempDir()
	code := application.Run(context.Background(), []string{
		"init", "--analyzer", "claude", "payments",
		"https://example.test/api.git", "-",
	})
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %s", code, errors.String())
	}
	if fake.initOptions.Project != "payments" || fake.initOptions.Analyzer.Provider != "claude" {
		t.Fatalf("Init options = %#v", fake.initOptions)
	}
	if len(fake.initOptions.Sources) != 2 || string(fake.initOptions.Sources[1].Content) != "# Project notes\n" {
		t.Fatalf("Init sources = %#v", fake.initOptions.Sources)
	}
}

func TestAddNamesSinglePastedSource(t *testing.T) {
	t.Parallel()
	var output, errors bytes.Buffer
	fake := &fakeKnowledgeService{}
	application := New(strings.NewReader("plain text"), &output, &errors)
	application.Service = fake
	code := application.Run(context.Background(), []string{
		"add", "--name", "meeting-notes", "payments", "-",
	})
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %s", code, errors.String())
	}
	if len(fake.addOptions.Sources) != 1 || fake.addOptions.Sources[0].Name != "meeting-notes" {
		t.Fatalf("Add options = %#v", fake.addOptions)
	}
}

func TestRefreshIsNonInteractive(t *testing.T) {
	t.Parallel()
	var output, errors bytes.Buffer
	fake := &fakeKnowledgeService{}
	application := New(strings.NewReader("unused"), &output, &errors)
	application.Service = fake
	if code := application.Run(context.Background(), []string{"refresh", "payments"}); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %s", code, errors.String())
	}
	if fake.refreshed != "payments" {
		t.Fatalf("refreshed project = %q", fake.refreshed)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	var output, errors bytes.Buffer
	application := New(strings.NewReader(""), &output, &errors)
	application.Version = Version{Version: "1.2.3", Commit: "abc123", Date: "2026-08-17"}
	if code := application.Run(context.Background(), []string{"version"}); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %s", code, errors.String())
	}
	if !strings.Contains(output.String(), `"version":"1.2.3"`) {
		t.Fatalf("version output = %q", output.String())
	}
}
