package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kimata1007/agent-ready/internal/integration"
	"github.com/kimata1007/agent-ready/internal/knowledge"
	"github.com/kimata1007/agent-ready/internal/project"
	"github.com/kimata1007/agent-ready/internal/source"
)

const maxStandardInputBytes = 20 << 20

type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type KnowledgeService interface {
	Init(context.Context, knowledge.InitOptions) (knowledge.Result, error)
	Add(context.Context, knowledge.AddOptions) (knowledge.Result, error)
	Refresh(context.Context, string) (knowledge.Result, error)
}

type Application struct {
	Input   io.Reader
	Output  io.Writer
	Errors  io.Writer
	Version Version
	Service KnowledgeService
	Root    string
}

func New(input io.Reader, output, errorsWriter io.Writer) *Application {
	return &Application{
		Input:   input,
		Output:  output,
		Errors:  errorsWriter,
		Version: Version{Version: "dev", Commit: "none", Date: "unknown"},
	}
}

func (a *Application) Run(ctx context.Context, arguments []string) int {
	if len(arguments) == 0 {
		a.printUsage(a.Errors)
		return 2
	}

	var err error
	switch arguments[0] {
	case "help", "-help", "--help", "-h":
		a.printUsage(a.Output)
		return 0
	case "version":
		err = a.runVersion(arguments[1:])
	case "init":
		err = a.runInit(ctx, arguments[1:])
	case "add":
		err = a.runAdd(ctx, arguments[1:])
	case "refresh":
		err = a.runRefresh(ctx, arguments[1:])
	default:
		err = fmt.Errorf("unknown command %q", arguments[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(a.Errors, "Error: %s\n", err)
		return 1
	}
	return 0
}

func (a *Application) runInit(ctx context.Context, arguments []string) error {
	flags := a.flagSet("init")
	provider := flags.String("analyzer", "codex", "analysis provider: codex or claude")
	model := flags.String("model", "", "optional provider model")
	name := flags.String("name", "", "name for a single source")
	jsonOutput := flags.Bool("json", false, "print machine-readable output")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() < 2 {
		return errors.New("init requires <project> and at least one <source>")
	}
	inputs, err := a.inputs(flags.Args()[1:], *name)
	if err != nil {
		return err
	}
	service, err := a.service()
	if err != nil {
		return err
	}
	result, err := service.Init(ctx, knowledge.InitOptions{
		Project: flags.Arg(0),
		Analyzer: project.AnalyzerConfig{
			Provider: strings.ToLower(strings.TrimSpace(*provider)),
			Model:    strings.TrimSpace(*model),
		},
		Sources: inputs,
	})
	if err != nil {
		return err
	}
	return a.writeResult("Initialized", result, *jsonOutput)
}

func (a *Application) runAdd(ctx context.Context, arguments []string) error {
	flags := a.flagSet("add")
	name := flags.String("name", "", "name for a single source")
	jsonOutput := flags.Bool("json", false, "print machine-readable output")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() < 2 {
		return errors.New("add requires <project> and at least one <source>")
	}
	inputs, err := a.inputs(flags.Args()[1:], *name)
	if err != nil {
		return err
	}
	service, err := a.service()
	if err != nil {
		return err
	}
	result, err := service.Add(ctx, knowledge.AddOptions{
		Project: flags.Arg(0),
		Sources: inputs,
	})
	if err != nil {
		return err
	}
	return a.writeResult("Updated", result, *jsonOutput)
}

func (a *Application) runRefresh(ctx context.Context, arguments []string) error {
	flags := a.flagSet("refresh")
	jsonOutput := flags.Bool("json", false, "print machine-readable output")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("refresh requires exactly one <project>")
	}
	service, err := a.service()
	if err != nil {
		return err
	}
	result, err := service.Refresh(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	verb := "Checked"
	if result.CatalogUpdated {
		verb = "Refreshed"
	}
	return a.writeResult(verb, result, *jsonOutput)
}

func (a *Application) runVersion(arguments []string) error {
	if len(arguments) != 0 {
		return errors.New("version accepts no arguments")
	}
	if err := json.NewEncoder(a.Output).Encode(a.Version); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	return nil
}

func (a *Application) inputs(values []string, requestedName string) ([]source.Input, error) {
	if requestedName != "" && len(values) != 1 {
		return nil, errors.New("--name can only be used with one source")
	}
	stdinCount := 0
	for _, value := range values {
		if value == "-" {
			stdinCount++
		}
	}
	if stdinCount > 1 {
		return nil, errors.New("standard input source '-' may only appear once")
	}
	var stdin []byte
	if stdinCount == 1 {
		content, err := io.ReadAll(io.LimitReader(a.Input, maxStandardInputBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read standard input: %w", err)
		}
		if len(content) > maxStandardInputBytes {
			return nil, errors.New("standard input exceeds 20 MiB")
		}
		stdin = content
	}
	inputs := make([]source.Input, 0, len(values))
	for _, value := range values {
		input := source.Input{Value: value}
		if len(values) == 1 {
			input.Name = strings.TrimSpace(requestedName)
		}
		if value == "-" {
			input.Content = stdin
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func (a *Application) service() (KnowledgeService, error) {
	if a.Service != nil {
		return a.Service, nil
	}
	root := a.Root
	if root == "" {
		var err error
		root, err = project.DefaultRoot()
		if err != nil {
			return nil, err
		}
	}
	store := project.Store{Root: root}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find user home: %w", err)
	}
	return knowledge.Service{
		Store:     store,
		Collector: source.Collector{Store: store},
		Integration: integration.Manager{
			Home:           home,
			AgentReadyRoot: root,
			CodexHome:      strings.TrimSpace(os.Getenv("CODEX_HOME")),
			ClaudeHome:     strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")),
		},
	}, nil
}

func (a *Application) writeResult(verb string, result knowledge.Result, jsonOutput bool) error {
	if jsonOutput {
		if err := json.NewEncoder(a.Output).Encode(result); err != nil {
			return fmt.Errorf("write result: %w", err)
		}
		return nil
	}
	fmt.Fprintf(a.Output, "%s project %q (%d sources).\n", verb, result.Project, result.SourceCount)
	if result.CatalogUpdated {
		root := a.Root
		if root == "" {
			root, _ = project.DefaultRoot()
		}
		fmt.Fprintf(a.Output, "Catalog: %s\n", filepath.Join(root, result.Project, "catalog.md"))
		fmt.Fprintf(a.Output, "Agent context: %s\n", filepath.Join(root, result.Project, "context.md"))
	}
	return nil
}

func (a *Application) flagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet("agent-ready "+name, flag.ContinueOnError)
	flags.SetOutput(a.Errors)
	return flags
}

func (a *Application) printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `agent-ready keeps project knowledge in ~/.agent-ready/<project>/.

Usage:
  agent-ready init [options] <project> <source...>
  agent-ready add [options] <project> <source...>
  agent-ready refresh [options] <project>

Sources:
  URL                 Git repository or web document
  path                Local file, Git repository, or directory
  -                   Markdown or plain text from standard input

Init options:
  --analyzer <name>   codex (default) or claude
  --model <name>      Optional provider model
  --name <name>       Name for a single source
  --json              Machine-readable output

Add options:
  --name <name>       Name for a single source
  --json              Machine-readable output

Refresh options:
  --json              Machine-readable output`)
}
