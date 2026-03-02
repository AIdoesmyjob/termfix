# termfix

A self-contained, fully offline AI coding assistant for the terminal. No API keys, no accounts, no internet required.

Termfix bundles a TUI chat interface, a local LLM inference server, and quantized language models into a single download. Extract, run, code.

> **Fork notice:** Termfix is a fork of [OpenCode](https://github.com/opencode-ai/opencode) by [Kujtim Hoxha](https://github.com/kujtimiihoxha) (now continued as [Crush](https://github.com/charmbracelet/crush) by the Charm team). The TUI, tool system, session management, and editor are built on OpenCode's foundation. Termfix modifies it to work fully offline with bundled local models.

## Quick Start

Download the archive for your platform from [Releases](https://github.com/AIdoesmyjob/termfix/releases), extract, and run:

```bash
# Linux
tar xzf termfix-linux-amd64.tar.gz
cd termfix-linux-amd64
./termfix.sh

# macOS (Apple Silicon)
tar xzf termfix-darwin-arm64.tar.gz
cd termfix-darwin-arm64
./termfix.sh

# macOS (Intel)
tar xzf termfix-darwin-amd64.tar.gz
cd termfix-darwin-amd64
./termfix.sh

# Windows — extract archive, run termfix.bat
```

That's it. The startup script handles everything: starts the inference server, waits for the model to load, generates config, and launches the TUI.

### macOS: Clear Quarantine Flag

If you downloaded the archive via a browser (Safari, Chrome, etc.), macOS Gatekeeper will block the binaries. The script will detect this and tell you the fix, but you can also do it upfront:

```bash
xattr -cr termfix-darwin-*/
```

This only needs to be done once after downloading. If you used `gh release download` or `curl` from the terminal, this step is usually unnecessary.

## What's in the Box

Each release archive (~1.7 GB) is fully self-contained:

| Component | Description |
|-----------|-------------|
| `termfix` | TUI chat interface (Go/[Bubble Tea](https://github.com/charmbracelet/bubbletea)) |
| `llama-server` | Local LLM inference ([llama.cpp](https://github.com/ggerganov/llama.cpp) b8182) |
| Shared libraries | CPU backend plugins (.so/.dylib/.dll) |
| `termfix.sh` / `termfix.bat` | Startup orchestrator |
| `models/` | Two bundled GGUF models (see below) |

### Bundled Models

| Model | Size | Notes |
|-------|------|-------|
| `termfix-cycle1-qwen15b-q4_k_m.gguf` | 941 MB | Default, best quality |
| `termfix-cycle1-llama1b-q4_k_m.gguf` | 771 MB | Faster, lighter |

An optional smaller model (`termfix-cycle1-qwen05b-q4_k_m.gguf`, 380 MB) is available as a separate download on the release page for low-RAM machines.

## Usage

### Interactive Mode

```bash
./termfix.sh
```

Opens a full-screen terminal UI. Type your message, press `Ctrl+S` to send. The model streams its response. Press `Ctrl+C` to exit.

### Single-Shot Mode

```bash
./termfix.sh -p "explain this error"
./termfix.sh -p "what does main.go do"
./termfix.sh -p "find the bug in app.py" -f json
```

Prints the response and exits. Useful for scripting.

### Command-Line Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--help` | `-h` | Show help |
| `--debug` | `-d` | Enable debug logging |
| `--cwd` | `-c` | Set working directory |
| `--prompt` | `-p` | Single-shot prompt (non-interactive) |
| `--output-format` | `-f` | Output format: `text` (default) or `json` |
| `--quiet` | `-q` | Hide spinner in non-interactive mode |
| `--version` | `-v` | Print version |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TERMFIX_MODEL` | auto-detect | Path to a specific `.gguf` model file |
| `TERMFIX_PORT` | `8012` | Port for the local llama-server |

```bash
# Use the smaller llama-1B model
TERMFIX_MODEL=./models/termfix-cycle1-llama1b-20260225T222748Z-q4_k_m.gguf ./termfix.sh

# Run two instances simultaneously
TERMFIX_PORT=8013 ./termfix.sh
```

## What It Can Do

The AI assistant has access to tools for working with your codebase:

| Tool | Description |
|------|-------------|
| `bash` | Execute shell commands |
| `view` | Read file contents |
| `write` | Create or overwrite files |
| `edit` | Make targeted edits to files |
| `glob` | Find files by pattern |
| `grep` | Search file contents |
| `ls` | List directory contents |
| `fetch` | Fetch content from URLs |
| `diagnostics` | Get LSP diagnostics |

**Note:** These tools work but the bundled 1-1.5B parameter models have limited ability to use them reliably. They work best for simple Q&A, code explanation, and straightforward tasks.

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+S` | Send message |
| `Ctrl+C` | Quit |
| `Ctrl+N` | New session |
| `Ctrl+X` | Cancel generation |
| `Ctrl+A` | Switch session |
| `Ctrl+K` | Command dialog |
| `Ctrl+O` | Model selection |
| `Ctrl+E` | Open external editor |
| `Ctrl+?` | Toggle help |
| `i` | Focus editor (when browsing messages) |
| `Esc` | Exit editor / close dialog |

## How It Works

1. `termfix.sh` finds a model in `models/` (prefers qwen-1.5B)
2. Starts `llama-server` on `localhost:8012` with the model
3. Waits for the server health check to pass
4. Auto-generates `.termfix.json` config mapping agents to the loaded model
5. Launches the TUI, which connects to the server via OpenAI-compatible API
6. On exit, the cleanup trap kills the server

Data is stored in:
- `~/.termfix/termfix.db` — session history (SQLite)
- `.termfix.json` (in install dir) — auto-generated agent config

## Building from Source

```bash
git clone https://github.com/AIdoesmyjob/termfix.git
cd termfix
go build -ldflags "-X 'github.com/opencode-ai/opencode/internal/version.Version=dev'" -o termfix
```

Requires Go 1.24+. You'll need to provide your own `llama-server` binary, shared libraries, and GGUF model files separately.

## Credits

Termfix is built on the work of others:

- **[OpenCode](https://github.com/opencode-ai/opencode)** by [Kujtim Hoxha](https://github.com/kujtimiihoxha) — the TUI, agent system, tool framework, session management, and editor that form the core of this project. OpenCode is now continued as [Crush](https://github.com/charmbracelet/crush) by the Charm team.
- **[llama.cpp](https://github.com/ggerganov/llama.cpp)** by [Georgi Gerganov](https://github.com/ggerganov) — the inference engine that makes local models practical.
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** by [Charm](https://github.com/charmbracelet) — the TUI framework.
- **[@isaacphi](https://github.com/isaacphi)** — LSP client implementation from [mcp-language-server](https://github.com/isaacphi/mcp-language-server).
- **[@adamdottv](https://github.com/adamdottv)** — UI/UX design direction for OpenCode.

## License

MIT License — see [LICENSE](LICENSE) for details. Original copyright belongs to Kujtim Hoxha.
