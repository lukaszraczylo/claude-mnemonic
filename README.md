# Claude Mnemonic

**Give Claude Code a memory that actually remembers.**

[![Release](https://img.shields.io/github/v/release/lukaszraczylo/claude-mnemonic?style=flat-square)](https://github.com/lukaszraczylo/claude-mnemonic/releases)
[![License](https://img.shields.io/github/license/lukaszraczylo/claude-mnemonic?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://go.dev)

---

Claude Code forgets everything when your session ends. Claude Mnemonic fixes that.

It captures what Claude learns during your coding sessions - bug fixes, architecture decisions, patterns that work - and brings that knowledge back in future conversations. No more re-explaining your codebase.

## Requirements

| Dependency | Required | Purpose |
|------------|----------|---------|
| **Claude Code CLI** | Yes | Host application (this is a plugin) |
| **jq** | Yes | JSON processing during installation |

That's it. No Python. No external services. Everything runs locally.

> **No API keys needed!** Claude Mnemonic uses Claude Code CLI, which works with your existing Claude Pro or Max subscription. No separate API costs.

## Install

**One command. That's it.**

```bash
curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash
```

<details>
<summary>Windows (PowerShell)</summary>

```powershell
irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.ps1 | iex
```
</details>

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/lukaszraczylo/claude-mnemonic.git
cd claude-mnemonic
make build && make install
```

Requires: Go 1.24+, Node.js 18+, CGO-compatible compiler
</details>

After install, open **http://localhost:37777** to see the dashboard. Start a new Claude Code session - memory is now active.

### Verifying Release Signatures

All release checksums are signed with [cosign](https://github.com/sigstore/cosign) using keyless signing. To verify:

```bash
# Download the checksum file and its sigstore bundle from the release
cosign verify-blob \
  --certificate-identity-regexp "https://github.com/lukaszraczylo/claude-mnemonic/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --bundle "checksums.txt.sigstore.json" \
  checksums.txt
```

## What it does

| Feature | Description |
|---------|-------------|
| **Persistent Memory** | Observations survive across sessions and restarts |
| **Project Isolation** | Each project has its own knowledge base |
| **Global Patterns** | Best practices are shared across all projects |
| **Semantic Search** | Find relevant context with natural language |
| **Live Statusline** | Real-time metrics in Claude Code: `[mnemonic] ● served:42 | project:28 memories` |
| **Web Dashboard** | Browse and manage memories at `localhost:37777` |

### How knowledge flows

```
You code with Claude
        ↓
Claude learns something useful
        ↓
Mnemonic captures it automatically
        ↓
Next session: Claude remembers
```

Behind the scenes: hooks capture Claude's observations → SQLite stores with full-text search → sqlite-vec enables semantic search with local embeddings (all-MiniLM-L6-v2) → relevant context is injected at session start.

## Configuration

Config file: `~/.claude-mnemonic/settings.json`

```json
{
  "CLAUDE_MNEMONIC_WORKER_PORT": 37777,
  "CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS": 100,
  "CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT": 25
}
```

| Variable | Default | What it does |
|----------|---------|--------------|
| `WORKER_PORT` | `37777` | Dashboard & API port |
| `CONTEXT_OBSERVATIONS` | `100` | Max memories per session |
| `CONTEXT_FULL_COUNT` | `25` | Full detail memories (rest are condensed to title only) |
| `CONTEXT_SESSION_COUNT` | `10` | Recent sessions to reference |

## Project vs Global scope

Observations are automatically scoped:

- **Project scope** (default) - stays within the project directory
- **Global scope** - shared everywhere

Global scope triggers on tags like: `best-practice`, `security`, `architecture`, `pattern`, `performance`

Example: A bug fix in your auth module stays local. "Always validate JWT server-side" goes global.

## MCP Tools

These search tools are available via MCP:

- `search` - semantic search across all memories
- `timeline` - browse by time
- `decisions` - find architecture decisions
- `changes` - find code modifications
- `how_it_works` - system understanding queries

## Troubleshooting

**Worker won't start?**
```bash
lsof -i :37777              # check if port is in use
cat /tmp/claude-mnemonic-worker.log  # view logs
```

**Database locked?**
```bash
rm -f ~/.claude-mnemonic/*.db-wal ~/.claude-mnemonic/*.db-shm
```

## Uninstall

```bash
# Remove everything
curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/uninstall.sh | bash

# Keep your data
curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/uninstall.sh | bash -s -- --keep-data
```

## Architecture

- **SQLite + FTS5** - Full-text search for exact matches
- **sqlite-vec** - Vector database embedded in SQLite
- **all-MiniLM-L6-v2** - Local embedding model (384 dimensions) via ONNX Runtime
- **Go** - Single binary, no external dependencies

Everything runs locally. No Python. No external vector database. No API calls for embeddings.

## Platform support

| Platform | Status |
|----------|--------|
| macOS Intel | Supported |
| macOS Apple Silicon | Supported |
| Linux amd64 | Supported |
| Linux arm64 | Supported |
| Windows amd64 | Supported |

## Development

```bash
make build          # build all
make test           # run tests
make dev            # dev mode with hot reload
make install        # install to Claude plugins
```

## License

MIT

---

**Links:** [Releases](https://github.com/lukaszraczylo/claude-mnemonic/releases) · [Issues](https://github.com/lukaszraczylo/claude-mnemonic/issues)
