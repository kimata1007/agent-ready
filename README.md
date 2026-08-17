# agent-ready

`agent-ready` maintains a global, source-backed knowledge catalog for coding agents.
Projects live under `~/.agent-ready/<project>/`; source repositories do not need an
agent-ready manifest.

The user-facing workflow has three non-interactive commands:

```sh
agent-ready init <project> <source...>
agent-ready add <project> <source...>
agent-ready refresh <project>
```

Sources may be Git or HTTP URLs, local files or directories, and `-` for Markdown
or plain text read from standard input. Codex and Claude Code adapters inspect new
or changed sources and generate a traceable catalog.
