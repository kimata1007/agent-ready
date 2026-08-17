package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
