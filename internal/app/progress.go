package app

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kimata1007/agent-ready/internal/knowledge"
)

const (
	interactiveProgressInterval = 200 * time.Millisecond
	logProgressInterval         = 15 * time.Second
)

type progressReporter struct {
	writer      io.Writer
	interactive bool
	interval    time.Duration
}

func newProgressReporter(writer io.Writer) *progressReporter {
	interactive := isTerminal(writer)
	interval := logProgressInterval
	if interactive {
		interval = interactiveProgressInterval
	}
	return &progressReporter{
		writer:      writer,
		interactive: interactive,
		interval:    interval,
	}
}

func (reporter *progressReporter) Start(task knowledge.ProgressTask) func(error) {
	if reporter == nil || reporter.writer == nil {
		return func(error) {}
	}

	started := time.Now()
	message := progressMessage(task)
	if reporter.interactive {
		fmt.Fprintf(reporter.writer, "\r\033[2K%s %s", spinnerFrames[0], message)
	} else {
		fmt.Fprintf(reporter.writer, "%s...\n", message)
	}

	interval := reporter.interval
	if interval <= 0 {
		interval = logProgressInterval
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		frame := 1
		for {
			select {
			case <-ticker.C:
				elapsed := formatElapsed(time.Since(started))
				if reporter.interactive {
					fmt.Fprintf(reporter.writer, "\r\033[2K%s %s (%s)", spinnerFrames[frame%len(spinnerFrames)], message, elapsed)
					frame++
				} else {
					fmt.Fprintf(reporter.writer, "Still %s (%s elapsed).\n", continuousProgressMessage(task), elapsed)
				}
			case <-stop:
				return
			}
		}
	}()

	var once sync.Once
	return func(operationErr error) {
		once.Do(func() {
			close(stop)
			<-stopped
			elapsed := formatElapsed(time.Since(started))
			completion := completedProgressMessage(task, operationErr != nil)
			if reporter.interactive {
				fmt.Fprintf(reporter.writer, "\r\033[2K%s %s (%s)\n", completionSymbol(operationErr), completion, elapsed)
			} else {
				fmt.Fprintf(reporter.writer, "%s %s (%s).\n", completionSymbol(operationErr), completion, elapsed)
			}
		})
	}
}

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func progressMessage(task knowledge.ProgressTask) string {
	prefix := progressPrefix(task)
	switch task.Phase {
	case knowledge.ProgressCollect:
		return fmt.Sprintf("%sCollecting %q", prefix, task.Source)
	case knowledge.ProgressAnalyze:
		provider := strings.TrimSpace(task.Provider)
		if provider == "" {
			return fmt.Sprintf("%sAnalyzing %q", prefix, task.Source)
		}
		return fmt.Sprintf("%sAnalyzing %q with %s", prefix, task.Source, provider)
	case knowledge.ProgressSynthesize:
		return fmt.Sprintf("Synthesizing catalog for %q from %d sources", task.Source, task.Total)
	default:
		return fmt.Sprintf("%sWorking on %q", prefix, task.Source)
	}
}

func continuousProgressMessage(task knowledge.ProgressTask) string {
	switch task.Phase {
	case knowledge.ProgressCollect:
		return fmt.Sprintf("collecting %q", task.Source)
	case knowledge.ProgressAnalyze:
		return fmt.Sprintf("analyzing %q", task.Source)
	case knowledge.ProgressSynthesize:
		return fmt.Sprintf("synthesizing the catalog for %q", task.Source)
	default:
		return fmt.Sprintf("working on %q", task.Source)
	}
}

func completedProgressMessage(task knowledge.ProgressTask, failed bool) string {
	if failed {
		switch task.Phase {
		case knowledge.ProgressCollect:
			return fmt.Sprintf("Failed to collect %q", task.Source)
		case knowledge.ProgressAnalyze:
			return fmt.Sprintf("Failed to analyze %q", task.Source)
		case knowledge.ProgressSynthesize:
			return fmt.Sprintf("Failed to synthesize the catalog for %q", task.Source)
		}
		return fmt.Sprintf("Failed while working on %q", task.Source)
	}

	switch task.Phase {
	case knowledge.ProgressCollect:
		return fmt.Sprintf("Collected %q", task.Source)
	case knowledge.ProgressAnalyze:
		return fmt.Sprintf("Analyzed %q", task.Source)
	case knowledge.ProgressSynthesize:
		return fmt.Sprintf("Synthesized the catalog for %q", task.Source)
	default:
		return fmt.Sprintf("Finished %q", task.Source)
	}
}

func progressPrefix(task knowledge.ProgressTask) string {
	if task.Current > 0 && task.Total > 0 {
		return fmt.Sprintf("[%d/%d] ", task.Current, task.Total)
	}
	return ""
}

func completionSymbol(operationErr error) string {
	if operationErr != nil {
		return "✗"
	}
	return "✓"
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return elapsed.Round(100 * time.Millisecond).String()
	}
	return elapsed.Round(time.Second).String()
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
