package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kimata1007/agent-ready/internal/knowledge"
)

func TestProgressReporterWritesStartAndCompletion(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	reporter := &progressReporter{
		writer:   &output,
		interval: time.Hour,
	}

	finish := reporter.Start(knowledge.ProgressTask{
		Phase:   knowledge.ProgressCollect,
		Source:  "architecture.md",
		Current: 1,
		Total:   2,
	})
	finish(nil)

	content := output.String()
	for _, want := range []string{
		`[1/2] Collecting "architecture.md"...`,
		`✓ Collected "architecture.md"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("progress output does not contain %q:\n%s", want, content)
		}
	}
}

func TestProgressReporterLeavesHeartbeatInNonTerminalLogs(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	reporter := &progressReporter{
		writer:   &output,
		interval: 5 * time.Millisecond,
	}

	finish := reporter.Start(knowledge.ProgressTask{
		Phase:    knowledge.ProgressAnalyze,
		Source:   "service",
		Provider: "codex",
		Current:  2,
		Total:    3,
	})
	time.Sleep(50 * time.Millisecond)
	finish(nil)

	content := output.String()
	if !strings.Contains(content, `Still analyzing "service"`) {
		t.Fatalf("progress output has no heartbeat:\n%s", content)
	}
}

func TestProgressReporterDoesNotRepeatOperationError(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	reporter := &progressReporter{
		writer:   &output,
		interval: time.Hour,
	}

	finish := reporter.Start(knowledge.ProgressTask{
		Phase:  knowledge.ProgressSynthesize,
		Source: "payments",
		Total:  2,
	})
	finish(errors.New("sensitive provider response"))

	content := output.String()
	if !strings.Contains(content, `✗ Failed to synthesize the catalog for "payments"`) {
		t.Errorf("progress output does not describe the failure:\n%s", content)
	}
	if strings.Contains(content, "sensitive provider response") {
		t.Errorf("progress output repeats the operation error:\n%s", content)
	}
}
