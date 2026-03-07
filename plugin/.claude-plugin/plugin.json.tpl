{
  "name": "claude-mnemonic",
  "version": "{{ .Version }}",
  "description": "Persistent memory system for Claude Code - Go implementation with SQLite and ChromaDB",
  "author": {
    "name": "lukaszraczylo",
    "email": "lukaszraczylo@users.noreply.github.com"
  },
  "hooks": "./hooks/hooks.json",
  "mcpServers": {
    "claude-mnemonic": {
      "command": "${CLAUDE_PLUGIN_ROOT}/mcp-server",
      "args": ["--project", "${CLAUDE_PROJECT}"],
      "env": {}
    }
  },
  "commands": ["./commands/restart.md"]
}
