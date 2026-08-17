# Architecture

## Responsibilities

agent-ready separates deterministic source tracking from AI-generated understanding.

```text
CLI arguments / stdin
        |
        v
source collector ----> immutable objects and source digests
        |
        v
Codex or Claude adapter ----> per-source structured analysis
        |
        v
catalog synthesis ----> catalog.json, catalog.md, context.md
        |
        v
global index ----> Codex AGENTS.md / Claude Code CLAUDE.md discovery
```

The collector owns source classification, retrieval, revision detection, and hashing. The analyzer
only receives a prepared read-only workspace and must produce output conforming to a fixed JSON
Schema. The knowledge service coordinates those components and persists a new catalog only after
all required analyses and project-level synthesis succeed.

## Commands and state transitions

### init

1. Validate the project name and analyzer provider.
2. Create `~/.agent-ready/<project>/`.
3. Collect and analyze every supplied source.
4. Synthesize the complete catalog.
5. Write project state and rebuild the global index.
6. Ensure the managed Codex and Claude Code instruction blocks exist.

If initialization fails, the newly created project directory is removed so the command can be
retried cleanly.

### add

1. Load the existing project.
2. Reject a source whose stable source ID is already registered.
3. Collect and analyze each new source.
4. Synthesize a catalog from both existing and new source analyses.
5. Replace the catalog and source registry, then rebuild the global index.

### refresh

1. Recollect every registered source.
2. Compare content digests and Git revisions.
3. Reanalyze only changed sources.
4. Skip synthesis when nothing changed.
5. Otherwise synthesize and persist the complete catalog.

Pasted text is immutable: refresh verifies its stored content-addressed object but does not request
new standard input.

## Analyzer boundary

Both providers implement the same interface:

```go
type Analyzer interface {
    Analyze(context.Context, project.Source, string) (project.SourceAnalysis, error)
    Synthesize(context.Context, project.Project, []project.SourceAnalysis) (project.ProjectAnalysis, error)
}
```

The Codex adapter invokes `codex exec` with a read-only sandbox, ephemeral sessions, and an output
schema. The Claude Code adapter invokes print mode with only read-oriented tools and the same output
schema. Authentication remains the responsibility of the installed provider CLI.

## Evidence model

Every catalog entry contains:

- a stable source ID;
- the source locator, content digest, and optional Git revision;
- an explanation of the source's purpose;
- important concepts;
- concrete paths or document headings;
- guidance describing when an agent should consult it.

The digest answers whether content changed. The analysis answers what the content means. Keeping
these separate lets refresh make a deterministic decision before spending an AI call.

## Startup discovery

`~/.agent-ready/index.json` contains only project names, generated context paths, catalog paths, and
source locators. A managed global instruction block tells Codex and Claude Code to match the current
directory or Git remote against this index. It does not copy generated knowledge into the global
instruction file and does not run an agent-ready command automatically.
