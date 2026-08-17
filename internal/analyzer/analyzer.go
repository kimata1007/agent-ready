package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kimata1007/agent-ready/internal/project"
)

type Analyzer interface {
	Analyze(context.Context, project.Source, string) (project.SourceAnalysis, error)
	Synthesize(context.Context, project.Project, []project.SourceAnalysis) (project.ProjectAnalysis, error)
}

type CommandResult struct {
	Stdout []byte
	Stderr []byte
}

type Runner interface {
	Run(context.Context, string, string, []string, []byte) (CommandResult, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(
	ctx context.Context,
	directory, name string,
	arguments []string,
	input []byte,
) (CommandResult, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()},
			fmt.Errorf("run %s: %w: %s", name, err, message)
	}
	return CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
}

type CommandAnalyzer struct {
	Provider string
	Model    string
	Runner   Runner
}

func New(settings project.AnalyzerConfig, runner Runner) (*CommandAnalyzer, error) {
	provider := strings.ToLower(strings.TrimSpace(settings.Provider))
	if provider != "codex" && provider != "claude" {
		return nil, errors.New("analyzer must be codex or claude")
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &CommandAnalyzer{Provider: provider, Model: settings.Model, Runner: runner}, nil
}

func (analyzer *CommandAnalyzer) Analyze(
	ctx context.Context,
	source project.Source,
	workspace string,
) (project.SourceAnalysis, error) {
	metadata, err := json.MarshalIndent(struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Locator  string `json:"locator"`
		Revision string `json:"revision,omitempty"`
	}{source.Name, source.Kind, source.Locator, source.Revision}, "", "  ")
	if err != nil {
		return project.SourceAnalysis{}, fmt.Errorf("encode source metadata: %w", err)
	}
	prompt := `Create a durable knowledge index for this registered source.

Work read-only. Inspect the source thoroughly enough that a future coding agent can find the
important documentation, entry points, modules, APIs, configuration, tests, constraints, and
operational guidance without guessing. Every location must be a real path or document heading
that exists in this source. Do not include secrets or personal information in the response.

Registered source:
` + string(metadata) + `

Return only the structured response required by the supplied JSON Schema.`

	var response analysisResponse
	if err := analyzer.runStructured(ctx, workspace, prompt, analysisSchema, &response); err != nil {
		return project.SourceAnalysis{}, err
	}
	if strings.TrimSpace(response.Summary) == "" || strings.TrimSpace(response.Purpose) == "" {
		return project.SourceAnalysis{}, errors.New("analyzer returned an incomplete source analysis")
	}
	for _, location := range response.Locations {
		if strings.TrimSpace(location.Path) == "" {
			return project.SourceAnalysis{}, errors.New("analyzer returned a location without a path")
		}
	}
	return project.SourceAnalysis{
		SchemaVersion: project.SchemaVersion,
		SourceID:      source.ID,
		Summary:       strings.TrimSpace(response.Summary),
		Purpose:       strings.TrimSpace(response.Purpose),
		KeyConcepts:   nonNil(response.KeyConcepts),
		Locations:     nonNilLocations(response.Locations),
		Usage:         nonNil(response.Usage),
		Warnings:      nonNil(response.Warnings),
	}, nil
}

func (analyzer *CommandAnalyzer) Synthesize(
	ctx context.Context,
	projectValue project.Project,
	analyses []project.SourceAnalysis,
) (project.ProjectAnalysis, error) {
	if len(analyses) == 0 {
		return project.ProjectAnalysis{}, errors.New("cannot synthesize an empty catalog")
	}
	directory, err := os.MkdirTemp("", "agent-ready-synthesis-*")
	if err != nil {
		return project.ProjectAnalysis{}, fmt.Errorf("create synthesis workspace: %w", err)
	}
	defer os.RemoveAll(directory)
	input := struct {
		Project string                   `json:"project"`
		Sources []project.SourceAnalysis `json:"sources"`
	}{Project: projectValue.Name, Sources: analyses}
	content, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return project.ProjectAnalysis{}, fmt.Errorf("encode synthesis input: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "catalog-input.json"), content, 0o600); err != nil {
		return project.ProjectAnalysis{}, fmt.Errorf("write synthesis input: %w", err)
	}
	prompt := `Read catalog-input.json and synthesize a concise project-level orientation for future
coding agents. Explain how the sources fit together, the main concepts and workflows, and exactly
when an agent should consult each source. Do not invent facts or locations. Do not include secrets
or personal information. Return only the structured response required by the JSON Schema.`
	var response project.ProjectAnalysis
	if err := analyzer.runStructured(ctx, directory, prompt, synthesisSchema, &response); err != nil {
		return project.ProjectAnalysis{}, err
	}
	if strings.TrimSpace(response.Overview) == "" {
		return project.ProjectAnalysis{}, errors.New("analyzer returned an empty project overview")
	}
	response.Overview = strings.TrimSpace(response.Overview)
	response.KeyConcepts = nonNil(response.KeyConcepts)
	response.Workflows = nonNil(response.Workflows)
	response.SourceGuidance = nonNil(response.SourceGuidance)
	return response, nil
}

func (analyzer *CommandAnalyzer) runStructured(
	ctx context.Context,
	directory, prompt, schema string,
	destination any,
) error {
	schemaFile, err := os.CreateTemp("", "agent-ready-schema-*.json")
	if err != nil {
		return fmt.Errorf("create analyzer schema: %w", err)
	}
	schemaPath := schemaFile.Name()
	defer os.Remove(schemaPath)
	if err := schemaFile.Chmod(0o600); err != nil {
		_ = schemaFile.Close()
		return fmt.Errorf("secure analyzer schema: %w", err)
	}
	if _, err := schemaFile.WriteString(schema); err != nil {
		_ = schemaFile.Close()
		return fmt.Errorf("write analyzer schema: %w", err)
	}
	if err := schemaFile.Close(); err != nil {
		return fmt.Errorf("close analyzer schema: %w", err)
	}

	var output []byte
	switch analyzer.Provider {
	case "codex":
		arguments := []string{
			"exec",
			"--ephemeral",
			"--skip-git-repo-check",
			"--sandbox", "read-only",
			"--color", "never",
			"--output-schema", schemaPath,
			"--cd", directory,
		}
		if strings.TrimSpace(analyzer.Model) != "" {
			arguments = append(arguments, "--model", analyzer.Model)
		}
		arguments = append(arguments, prompt)
		result, err := analyzer.Runner.Run(ctx, directory, "codex", arguments, nil)
		if err != nil {
			return err
		}
		output = result.Stdout
	case "claude":
		arguments := []string{
			"--print",
			"--no-session-persistence",
			"--permission-mode", "dontAsk",
			"--tools", "Read,Glob,Grep",
			"--disable-slash-commands",
			"--no-chrome",
			"--output-format", "json",
			"--json-schema", schema,
		}
		if strings.TrimSpace(analyzer.Model) != "" {
			arguments = append(arguments, "--model", analyzer.Model)
		}
		arguments = append(arguments, prompt)
		result, err := analyzer.Runner.Run(ctx, directory, "claude", arguments, nil)
		if err != nil {
			return err
		}
		output, err = claudeStructuredOutput(result.Stdout)
		if err != nil {
			return err
		}
	}
	if err := json.Unmarshal(output, destination); err != nil {
		return fmt.Errorf("decode %s structured output: %w", analyzer.Provider, err)
	}
	return nil
}

func claudeStructuredOutput(output []byte) ([]byte, error) {
	var envelope struct {
		StructuredOutput json.RawMessage `json:"structured_output"`
		Result           json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("decode claude output envelope: %w", err)
	}
	if len(envelope.StructuredOutput) != 0 && string(envelope.StructuredOutput) != "null" {
		return envelope.StructuredOutput, nil
	}
	if len(envelope.Result) == 0 {
		return nil, errors.New("claude output did not contain structured_output or result")
	}
	var result string
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return nil, errors.New("claude result was not structured JSON")
	}
	return []byte(result), nil
}

type analysisResponse struct {
	Summary     string             `json:"summary"`
	Purpose     string             `json:"purpose"`
	KeyConcepts []string           `json:"keyConcepts"`
	Locations   []project.Location `json:"locations"`
	Usage       []string           `json:"usage"`
	Warnings    []string           `json:"warnings"`
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilLocations(values []project.Location) []project.Location {
	if values == nil {
		return []project.Location{}
	}
	return values
}

const analysisSchema = `{
  "type": "object",
  "properties": {
    "summary": {"type": "string"},
    "purpose": {"type": "string"},
    "keyConcepts": {"type": "array", "items": {"type": "string"}},
    "locations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "path": {"type": "string"},
          "description": {"type": "string"}
        },
        "required": ["path", "description"],
        "additionalProperties": false
      }
    },
    "usage": {"type": "array", "items": {"type": "string"}},
    "warnings": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["summary", "purpose", "keyConcepts", "locations", "usage", "warnings"],
  "additionalProperties": false
}`

const synthesisSchema = `{
  "type": "object",
  "properties": {
    "overview": {"type": "string"},
    "keyConcepts": {"type": "array", "items": {"type": "string"}},
    "workflows": {"type": "array", "items": {"type": "string"}},
    "sourceGuidance": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["overview", "keyConcepts", "workflows", "sourceGuidance"],
  "additionalProperties": false
}`
