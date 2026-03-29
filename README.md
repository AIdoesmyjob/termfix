# termfix

A self-contained, fully offline AI system troubleshooting assistant for the terminal. No API keys, no accounts, no internet required.

Termfix bundles a TUI chat interface, a local LLM inference server, and a fine-tuned diagnostic model into a single download. Extract, run, diagnose.

> **Fork notice:** Termfix is a fork of [OpenCode](https://github.com/opencode-ai/opencode) by [Kujtim Hoxha](https://github.com/kujtimiihoxha) (now continued as [Crush](https://github.com/charmbracelet/crush) by the Charm team). The TUI, tool system, session management, and editor are built on OpenCode's foundation. Termfix modifies it to work fully offline with a bundled local model purpose-trained for system diagnostics.

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

This only needs to be done once after downloading.

## What's in the Box

Each release archive (~600 MB) is fully self-contained:

| Component | Description |
|-----------|-------------|
| `termfix` | TUI chat interface (Go/[Bubble Tea](https://github.com/charmbracelet/bubbletea)) |
| `llama-server` | Local LLM inference ([llama.cpp](https://github.com/ggerganov/llama.cpp) b8500) |
| Shared libraries | CPU backend plugins (.so/.dylib/.dll) |
| `termfix.sh` / `termfix.bat` | Startup orchestrator |
| `models/` | Fine-tuned diagnostic model (see below) |

### Bundled Model: termfix-cycle8 (Qwen 3.5 0.8B fine-tune)

| Property | Value |
|----------|-------|
| Base model | Qwen 3.5 0.8B (text-only) |
| Fine-tuning | Unsloth LoRA on 2000+ diagnostic examples |
| Quantization | Q4_K_M (4-bit) |
| Size | 505 MB |
| Context window | 8192 tokens |
| Inference speed | ~200 tok/s on M4, ~30 tok/s on older Intel |

The model is trained specifically for system troubleshooting: selecting the right diagnostic command, executing it, and analyzing the real output. It handles tool calling natively using Qwen's XML format.

## Architecture: Two-Pass Design for Small Models

Termfix uses a **two-pass architecture** designed specifically for reliable tool calling with sub-1B parameter models:

```
Pass 1: Tool Selection (with tool definitions)
  [system prompt + tools + user query] → model picks a tool + arguments
  Non-streaming request, grammar-constrained tool parsing

Pass 2: Diagnostic Generation (no tools, fresh context)
  [system prompt + user query + tool output] → model analyzes results
  Non-streaming request, text-only generation
```

**Why two passes?** Small models (0.6-1.5B) struggle with multi-turn tool calling — the growing conversation history of tool calls and results fills the context window and causes generation loops. By splitting into two independent, fresh-context passes, each pass is short and focused (exactly what small models are good at).

Key design decisions:
- **Non-streaming** for both passes — llama-server's PEG grammar parser only reliably detects tool calls in non-streaming mode
- **No tool definitions in Pass 2** — saves ~4000 tokens, prevents the model from trying to make another tool call
- **Input sanitization** — extracts the first clean command line from hallucinated multi-line parameters
- **Compact tool descriptions** — 1-line descriptions instead of the full 150-line originals (saves ~3500 tokens)
- **Repetition truncation** — detects and stops generation loops in small model output

## Usage

### Interactive Mode

```bash
./termfix.sh
```

Opens a full-screen terminal UI. Type your message, press `Ctrl+S` to send.

### Single-Shot Mode

```bash
./termfix.sh -p "check disk usage" -q
./termfix.sh -p "what is DNS" -q
./termfix.sh -p "show me /etc/hosts" -q
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
# Use a specific model
TERMFIX_MODEL=./models/my-custom-model.gguf ./termfix.sh

# Run two instances simultaneously
TERMFIX_PORT=8013 ./termfix.sh
```

## Tools

The diagnostic assistant has access to read-only inspection tools:

| Tool | Description |
|------|-------------|
| `bash` | Execute shell commands (df, ps, top, uname, etc.) |
| `view` | Read file contents with line numbers |
| `glob` | Find files by pattern |
| `grep` | Search file contents |

## How It Works

1. `termfix.sh` finds the model in `models/` (prefers Qwen 3.5 models)
2. Starts `llama-server` on `localhost:8012` with optimized sampling params
3. Waits for the server health check to pass
4. Auto-generates `.termfix.json` config mapping agents to the loaded model
5. Launches the TUI, which connects to the server via OpenAI-compatible API
6. User query → Pass 1 (tool selection) → tool execution → Pass 2 (diagnostic) → response
7. On exit, the cleanup trap kills the server

Data is stored in:
- `~/.termfix/termfix.db` — session history (SQLite)
- `.termfix.json` (in install dir) — auto-generated agent config

## Training

The model was fine-tuned using [Unsloth](https://github.com/unslothai/unsloth) with LoRA on a 3090 GPU. Training data, generation scripts, and config are in `training/`.

Key training details:
- 2000+ examples covering bash commands, file viewing, pattern search, and knowledge questions
- Native Qwen XML tool calling format (`<function=name><parameter=key>value</parameter></function>`)
- Trained with the exact system prompt and tool definitions used in production
- 8 training cycles with automated quality gates (tool selection accuracy, grounding, hallucination detection)

To fine-tune your own model, see `training/generate_data.py` for the data format and `training/train_config.yaml` for Unsloth configuration.

## Building from Source

```bash
git clone https://github.com/AIdoesmyjob/termfix.git
cd termfix
go build -ldflags "-X 'github.com/opencode-ai/opencode/internal/version.Version=dev'" -o termfix
```

Requires Go 1.24+. You'll need to provide your own `llama-server` binary, shared libraries, and GGUF model files separately.

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

## Credits

Termfix is built on the work of others:

- **[OpenCode](https://github.com/opencode-ai/opencode)** by [Kujtim Hoxha](https://github.com/kujtimiihoxha) — the TUI, agent system, tool framework, session management, and editor that form the core of this project. OpenCode is now continued as [Crush](https://github.com/charmbracelet/crush) by the Charm team.
- **[llama.cpp](https://github.com/ggerganov/llama.cpp)** by [Georgi Gerganov](https://github.com/ggerganov) — the inference engine that makes local models practical.
- **[Unsloth](https://github.com/unslothai/unsloth)** — fast LoRA fine-tuning used to train the diagnostic model.
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** by [Charm](https://github.com/charmbracelet) — the TUI framework.
- **[@isaacphi](https://github.com/isaacphi)** — LSP client implementation from [mcp-language-server](https://github.com/isaacphi/mcp-language-server).
- **[@adamdottv](https://github.com/adamdottv)** — UI/UX design direction for OpenCode.

## License

MIT License — see [LICENSE](LICENSE) for details. Original copyright belongs to Kujtim Hoxha.
