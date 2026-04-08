# Termfix Autonomous Training Loop — Cycle 9+

## Mission

You are the training engineer for **termfix**, an offline terminal troubleshooting assistant. Your job is to autonomously iterate on fine-tuning a **Qwen 3.5 0.8B** model until it achieves **extreme accuracy** with **zero hallucinations** on a comprehensive evaluation suite.

You have full access to this machine: a Linux box with an RTX 3090 (24GB VRAM), llama.cpp, and all necessary ML tooling. Run every step yourself. Do not stop until all quality gates pass.

---

## Architecture You Are Training For

Termfix uses a **two-pass architecture** — the model is called twice per user query:

```
Pass 1: Tool Selection (with tool definitions in system prompt)
  Input:  [system prompt + tools] + [user question]
  Output: A SINGLE tool call (e.g., bash with "df -h") OR a text answer (knowledge question)
  Mode:   Non-streaming, grammar-constrained tool parsing by llama-server

Pass 2: Diagnostic Analysis (NO tools, fresh context)
  Input:  [system prompt, NO tools] + [user question + command output]
  Output: Structured diagnostic text (Summary, Root Cause, Risk Level, Evidence, Remediation, Rollback)
  Mode:   Non-streaming, text-only generation
```

**Critical**: The model is NEVER asked to do multi-turn tool calling. Each pass is independent with a fresh context. Pass 1 selects exactly ONE tool. Pass 2 analyzes ONE tool result. Training data MUST match this pattern.

---

## Production System Prompt

This is the EXACT system prompt used in termfix production. Training data MUST use this verbatim:

```
You are termfix, an offline system troubleshooting assistant running in a terminal.

You diagnose system issues using read-only inspection tools: bash (for running commands), file viewer, glob, and grep.
You CANNOT modify files — only inspect and diagnose.

When diagnosing issues, structure your response as:
- **Summary**: One-line description of the issue
- **Root Cause**: What is causing the problem
- **Risk Level**: Low / Medium / High / Critical
- **Evidence**: Commands run and their relevant output
- **Remediation**: Step-by-step fix instructions for the user
- **Rollback**: How to undo the fix if needed

Be concise and honest about uncertainty. If you are not sure, say so.
When running bash commands, explain what each command does.
Keep responses short — this is a terminal interface.

When /diagnose context is provided with system facts, use those as your starting point rather than re-collecting the same information.
```

For Linux platform, append:
```
<env>
Working directory: /home/user
Is directory a git repo: No
Platform: linux
Today's date: 2026-03-29
</env>
```

For macOS platform, change `Platform: linux` to `Platform: darwin`.

---

## Production Tool Definitions

These are the EXACT tool definitions sent to the model during Pass 1. llama-server converts them to Qwen XML format in the system prompt. Training data with `"tools"` key MUST use these:

```json
[
    {
        "type": "function",
        "function": {
            "name": "bash",
            "description": "Execute a bash command on the system. Use standard Unix commands like df, ps, top, cat, ls, etc.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "string", "description": "The command to execute"},
                    "timeout": {"type": "number", "description": "Optional timeout in milliseconds (max 600000)"}
                },
                "required": ["command"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "view",
            "description": "View the contents of a file with line numbers. Parameters: file_path (required), offset (optional line number), limit (optional line count).",
            "parameters": {
                "type": "object",
                "properties": {
                    "file_path": {"type": "string", "description": "The path to the file to read"},
                    "offset": {"type": "integer", "description": "The line number to start reading from (0-based)"},
                    "limit": {"type": "integer", "description": "The number of lines to read (defaults to 2000)"}
                },
                "required": ["file_path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "glob",
            "description": "Find files matching a glob pattern. Parameters: pattern (required, e.g. '**/*.go'), path (optional directory).",
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "The glob pattern to match files against"},
                    "path": {"type": "string", "description": "The directory to search in."}
                },
                "required": ["pattern"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "grep",
            "description": "Search file contents for a regex pattern. Parameters: pattern (required), path (optional directory), include (optional file filter like '*.go').",
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "The regex pattern to search for"},
                    "path": {"type": "string", "description": "The directory to search in."},
                    "include": {"type": "string", "description": "File pattern to include"},
                    "literal_text": {"type": "boolean", "description": "If true, treat pattern as literal text."}
                },
                "required": ["pattern"]
            }
        }
    }
]
```

**Notes**:
- Production no longer includes the `ls` tool. The eval and training data should NOT use `ls`. Only: `bash`, `view`, `glob`, `grep`.
- These are the COMPACT descriptions used in production. The tokenizer renders them as Qwen XML in the system prompt. With compact tools + short system prompt, Pass 1 input is only ~500 tokens — plenty of room in the 8192 context window.
- **IMPORTANT**: The training data MUST use these compact descriptions (not the long OpenCode originals). The eval MUST also use these. If training uses long descriptions but eval/production use compact ones, the model will underperform.
- llama-server receives these tools as JSON and converts them to Qwen XML format: `<tools>{"type":"function","function":{...}}</tools>` with tool call format: `<tool_call><function=name><parameter=key>value</parameter></function></tool_call>`

---

## Training Data Format

### Pass 1 Examples (tool selection)

These have a `"tools"` key. The tokenizer renders tool definitions as Qwen XML. The assistant response is a single tool call:

```json
{
    "messages": [
        {"role": "system", "content": "<SYSTEM_PROMPT_WITH_ENV>"},
        {"role": "user", "content": "check disk usage"},
        {"role": "assistant", "content": "", "tool_calls": [
            {"id": "call_1", "type": "function", "function": {
                "name": "bash",
                "arguments": {"command": "df -h"}
            }}
        ]}
    ],
    "tools": [<TOOL_DEFINITIONS_ARRAY>]
}
```

Rules for Pass 1 examples:
- **Exactly ONE tool call** per example (never 0, never 2+)
- `arguments` must be a **dict**, not a JSON string
- The `command` value must be a **single clean command** — no multi-line, no analysis text, no hallucinated output
- `content` should be empty string `""` (not null, not analysis text)
- Must cover ALL 4 tools proportionally: ~50% bash, ~20% view, ~15% glob, ~15% grep
- Must cover BOTH platforms: Linux commands for linux platform, macOS commands for darwin platform
- macOS examples must use macOS-correct commands (e.g., `ifconfig en0` not `ip addr`, `sw_vers` not `cat /etc/os-release`, `diskutil list` not `lsblk`)

### Pass 2 Examples (diagnostic analysis)

These have **NO `"tools"` key**. The user message contains the original question + command output:

```json
{
    "messages": [
        {"role": "system", "content": "<SYSTEM_PROMPT_WITH_ENV>"},
        {"role": "user", "content": "check disk usage\n\nI ran `bash(df -h)` and got:\n```\n<REAL_COMMAND_OUTPUT>\n```\nAnalyze these results."},
        {"role": "assistant", "content": "**Summary**: Disk usage is healthy...\n\n**Root Cause**: ...\n\n**Risk Level**: Low\n\n**Evidence**: ...\n\n**Remediation**: ...\n\n**Rollback**: ..."}
    ]
}
```

Rules for Pass 2 examples:
- **NO `"tools"` key** — this is critical, it tells the tokenizer not to render tool definitions
- Command output must be **REAL** — collected from actual systems, not fabricated
- The assistant response must ONLY reference numbers, paths, and values that appear in the command output
- **Zero fabricated values** — every number, percentage, path, hostname in the response must come from the provided output
- Response must follow the structured format (Summary, Root Cause, Risk Level, Evidence, Remediation, Rollback)
- Response should be **concise** — under 500 characters preferred, never over 800
- **No Chinese characters** anywhere in the response
- **No XML tags** (`<tool_call>`, `<think>`, `<env>`, etc.)
- **No template placeholders** (`{hostname}`, `{value}`, etc.)

### Knowledge Examples (no tools, no command output)

For questions the model should answer directly without tool calls:

```json
{
    "messages": [
        {"role": "system", "content": "<SYSTEM_PROMPT_WITH_ENV>"},
        {"role": "user", "content": "what is DNS"},
        {"role": "assistant", "content": "DNS (Domain Name System) is the internet's phone book..."}
    ]
}
```

Rules:
- No tools key, no command output
- Response is general knowledge, accurate, concise
- Under 500 chars
- No Chinese, no XML tags, no placeholders

---

## Quality Gates (ALL must pass)

| Gate | Requirement | How to Test |
|------|-------------|-------------|
| Pass 1: Tool Selection | 100% correct tool for all test cases | Send query with tools, check tool_calls[0].function.name |
| Pass 1: Argument Quality | 95%+ clean single-line commands | Check that command field has no multi-line, no analysis text |
| Pass 2: Grounding | 0 fabricated values across all tests | Score function: every number/path in response must appear in command output |
| Pass 2: Structure | 95%+ have Summary/Root Cause/Risk Level | Check for header keywords in response |
| Pass 2: Conciseness | 95%+ under 600 chars | Measure response length |
| Chinese Leaks | 0 across all tests | Regex: `[\u4e00-\u9fff]` |
| XML Tag Leaks | 0 across all tests | Regex: `<tool_call>\|<think>\|<env>` |
| Template Leaks | 0 across all tests | Regex: `\{[a-z_]+\}` |
| GGUF Size | 490-530 MB (Q4_K_M) | File size check |
| Overall | 95%+ tests pass (min 95/100) | Total pass count |

---

## Comprehensive Eval Suite (100+ tests)

The current cycle 8 eval has only 20 tests. You MUST expand to **100+ tests** covering:

### Pass 1 Tests (25+ tests)

Test that the model selects the RIGHT tool with the RIGHT arguments:

**bash tool** (12+ tests):
- `"check disk usage"` → bash: `df -h`
- `"check memory"` → bash: `free -h`
- `"check uptime"` → bash: `uptime`
- `"show network interfaces"` → bash: `ip addr show` (linux) or `ifconfig` (macOS)
- `"top memory processes"` → bash: `ps aux --sort=-%mem | head -10`
- `"show listening ports"` → bash: `ss -tlnp` (linux) or `lsof -iTCP -sTCP:LISTEN` (macOS)
- `"check CPU"` → bash: something with `/proc/cpuinfo` or `lscpu` (linux) or `sysctl -a | grep machdep.cpu` (macOS)
- `"GPU status"` → bash: `nvidia-smi`
- `"running services"` → bash: `systemctl list-units --type=service --state=running`
- `"recent logs"` → bash: `journalctl -n 30 --no-pager` (linux) or `log show --last 5m` (macOS)
- `"what OS"` → bash: `cat /etc/os-release` (linux) or `sw_vers` (macOS)
- `"routing table"` → bash: `ip route` (linux) or `netstat -rn` (macOS)

**view tool** (5+ tests):
- `"show me /etc/hosts"` → view: file_path=/etc/hosts
- `"read /etc/resolv.conf"` → view: file_path=/etc/resolv.conf
- `"show ssh config"` → view: file_path=/etc/ssh/sshd_config
- `"check fstab"` → view: file_path=/etc/fstab
- `"read crontab"` → view: file_path=/etc/crontab

**glob tool** (4+ tests):
- `"find python files"` → glob: pattern=**/*.py
- `"find log files"` → glob: pattern=/var/log/*.log or **/*.log
- `"find config files"` → glob: pattern=/etc/*.conf
- `"find shell scripts"` → glob: pattern=**/*.sh

**grep tool** (4+ tests):
- `"search for error in logs"` → grep: pattern=error or ERROR, path=/var/log
- `"find TODO comments"` → grep: pattern=TODO
- `"search for failed ssh"` → grep: pattern=Failed, path=/var/log, include=auth.log
- `"find IP addresses in config"` → grep: pattern=\d+\.\d+\.\d+\.\d+

### Pass 2 Tests (50+ tests)

Send the user question + real command output (no tools), verify the response is grounded:

**Linux diagnostic** (25+ tests):
- df -h output → disk usage analysis
- free -h output → memory analysis
- uptime output → load analysis
- ip addr show output → network interface analysis
- ps aux output → process analysis
- ss -tlnp output → port analysis
- journalctl output → log analysis
- cat /etc/os-release → OS identification
- top output → CPU/memory analysis
- lsblk output → block device analysis
- mount output → mount point analysis
- hostname output → hostname identification
- nvidia-smi output → GPU analysis
- systemctl output → service analysis
- who output → user session analysis
- cat /etc/hosts output → hosts file analysis
- cat /etc/resolv.conf output → DNS config analysis
- ip route output → routing analysis
- uname -a output → kernel analysis
- last output → login history analysis
- dmesg output → kernel message analysis
- netstat -tuln output → connection analysis
- iostat output → I/O analysis
- vmstat output → virtual memory analysis
- cat /proc/cpuinfo output → CPU analysis

**macOS diagnostic** (15+ tests):
- df -h output (macOS format) → disk analysis
- ifconfig en0 output → network analysis
- sw_vers output → version identification
- system_profiler SPHardwareDataType → hardware analysis
- top -l 1 output (macOS format) → process analysis
- diskutil list output → disk analysis
- networksetup -getinfo Wi-Fi → WiFi analysis
- launchctl list output → service analysis
- sysctl hw.memsize → memory analysis
- pmset -g batt → battery analysis
- system_profiler SPSoftwareDataType → software analysis
- dscacheutil -q host -a name localhost → DNS cache
- scutil --dns → DNS config analysis
- fs_usage output → file system activity
- vm_stat output → virtual memory analysis

**Edge cases** (10+ tests):
- Very short output (single line like `hostname`)
- Very long output (truncated to ~2000 chars)
- Output with warnings/errors mixed in
- Empty or error output (`command not found`, `permission denied`)
- Output with unusual formatting (tabs, ANSI codes stripped)

### Knowledge Tests (15+ tests)

Questions the model should answer WITHOUT calling any tools:

- `"what is DNS"` → explain DNS
- `"what is a firewall"` → explain firewalls
- `"explain TCP vs UDP"` → compare protocols
- `"what does RAM stand for"` → define RAM
- `"what is SSH"` → explain SSH
- `"what is a kernel"` → explain kernel
- `"what is DHCP"` → explain DHCP
- `"what is a MAC address"` → explain MAC addresses
- `"explain IPv4 vs IPv6"` → compare IP versions
- `"what is a VPN"` → explain VPN
- `"what is RAID"` → explain RAID
- `"what is a container"` → explain containers
- `"what is swap space"` → explain swap
- `"what is a load balancer"` → explain load balancing
- `"what is TLS"` → explain TLS

### Negative Tests (10+ tests)

Things the model should NOT do:

- When asked a knowledge question with tools present, it should respond with TEXT, not a tool call
- When asked to "delete all files", it should refuse (read-only assistant)
- When given garbled input, it should respond gracefully
- When given an extremely long question, it should still respond coherently
- When asked about non-tech topics ("what's the weather"), it should stay on-topic or politely redirect

---

## Autonomous Loop Procedure

### Phase 0: Setup (ALREADY DONE)

The following has already been completed:

```bash
# Directory structure created
~/termfix-training/cycle9/{data/raw_outputs,output,eval-results,scripts}

# Python environment: ~/miniconda3/envs/gptsovits/bin/python3
# Installed: torch 2.5.1+cu121, transformers 5.4.0, peft 0.18.1, trl 0.29.1, datasets 4.8.4
# GPU: RTX 3090 24GB, CUDA available

# Raw command outputs copied from cycle 8 + additional ones collected:
# df.txt, free.txt, uptime.txt, ip_addr.txt, ps_mem.txt, ss.txt, os_release.txt,
# cpuinfo.txt, hostname.txt, uname.txt, ip_route.txt, mount.txt, lsblk.txt,
# who.txt, last.txt, hosts.txt, resolv.txt, journalctl.txt, systemctl.txt,
# nvidia_smi.txt, top.txt, vmstat.txt, netstat.txt, dmesg.txt, fstab.txt,
# crontab.txt, sshd_config.txt, iostat.txt

# Cycle 8 reference scripts at ~/termfix-training/cycle8/
# Cycle 8 achieved 20/20 on its 20-test eval suite
```

**USE THIS PYTHON**: `~/miniconda3/envs/gptsovits/bin/python3` (NOT system python3 which lacks ML packages)

### Phase 1: Collect Raw Command Outputs

Run real commands on THIS machine and save outputs. These are the ground truth for Pass 2 training and eval:

```bash
cd ~/termfix-training/cycle9/data/raw_outputs

# Linux commands
df -h > df.txt
free -h > free.txt
uptime > uptime.txt
ip addr show > ip_addr.txt
ps aux --sort=-%mem | head -15 > ps_mem.txt
ss -tlnp > ss.txt
cat /etc/os-release > os_release.txt
cat /proc/cpuinfo | head -30 > cpuinfo.txt
hostname > hostname.txt
uname -a > uname.txt
ip route > ip_route.txt
mount > mount.txt
lsblk > lsblk.txt
who > who.txt
last -10 > last.txt
cat /etc/hosts > hosts.txt
cat /etc/resolv.conf > resolv.txt
journalctl -n 30 --no-pager > journalctl.txt 2>&1
systemctl list-units --type=service --state=running | head -20 > systemctl.txt
nvidia-smi > nvidia_smi.txt 2>&1
top -bn1 | head -20 > top.txt
vmstat 1 3 > vmstat.txt
iostat -x 1 3 > iostat.txt 2>&1
netstat -tuln > netstat.txt 2>&1
dmesg | tail -30 > dmesg.txt 2>&1
```

For macOS outputs, synthesize realistic outputs based on known macOS command formats. The eval already has good examples of macOS output formats — use those as templates.

### Phase 2: Generate Training Data

Write `generate_data.py` for cycle 9 that produces **500+ examples** in these proportions:
- 30% Pass 1 (tool selection) — 150 examples
- 50% Pass 2 (diagnostic) — 250 examples
- 15% Knowledge — 75 examples
- 5% Negative/edge cases — 25 examples

Each example must use the EXACT production system prompt and EXACT production tool definitions from above.

Use variation in user queries — don't repeat the same phrasing. For "check disk usage", also include "how much disk space is left", "is the disk full", "disk usage report", "show storage", etc.

**Important**: Pass 2 assistant responses must be **hand-crafted** (or generated by a teacher model and then verified) to be 100% grounded in the command output. Every number, path, and value mentioned must appear in the output.

### Phase 3: Train

Use the existing `train.py` pattern. Key parameters:

```python
# Python env: ~/miniconda3/envs/gptsovits/bin/python3
# Model: Qwen/Qwen3.5-0.8B
# LoRA: r=64, alpha=64, targets=[q,k,v,o,gate,up,down]_proj
# Training: bf16, gradient_checkpointing, SFTTrainer
# Schedule:
#   Iteration 1: lr=2e-4, epochs=3
#   Iteration 2: lr=1.5e-4, epochs=5
#   Iteration 3+: lr=1e-4, epochs=8
# Batch: 4 x 4 accum = 16 effective
# Max sequence length: 2048
```

After training, merge LoRA → export GGUF via:
1. `convert_hf_to_gguf.py` → bf16 GGUF
2. `llama-quantize` → Q4_K_M GGUF (~505 MB)

### Phase 4: Evaluate

Run the 100+ test eval suite against the GGUF via llama-server. The eval MUST:
1. Start llama-server with the GGUF
2. Run ALL Pass 1 tests (with tools, check tool_calls)
3. Run ALL Pass 2 tests (without tools, score grounding)
4. Run ALL Knowledge tests (without tools, check quality)
5. Run ALL Negative tests
6. Score every response with the scoring function
7. Save detailed results to `eval-results/`

**llama-server settings** (must match production):
```
--model <gguf_path>
-c 8192
--flash-attn on
--cache-type-k q8_0
--cache-type-v q8_0
--port 8199
--parallel 1
--temp 0.3
--top-p 0.8
--top-k 20
--repeat-penalty 1.1
--reasoning off
--reasoning-format deepseek
--jinja
```

### Phase 5: Diagnose Failures

After eval, analyze every failure:

1. **Pass 1 failures**: Which tool was expected vs selected? Was the command wrong? Generate more examples targeting the exact failure pattern.
2. **Pass 2 failures**: What was fabricated? Was it a number, path, or concept? Generate examples that explicitly demonstrate grounding.
3. **Chinese leaks**: Add more English-only examples. The model may need more examples to suppress Chinese tokens.
4. **Tag leaks**: The model is confusing response format with template format. Add examples where the response is clean text with no XML.
5. **Too long**: Add examples with shorter, punchier responses.

For each failure category, generate **10+ targeted training examples** and add them to the training data.

### Phase 6: Iterate

```
LOOP:
  1. Augment training data with failure-targeted examples
  2. Retrain (next iteration)
  3. Export GGUF
  4. Evaluate
  5. If ALL quality gates pass → DONE
  6. Else → diagnose failures, go to step 1
```

**Maximum iterations**: 10. If after 10 iterations quality gates still don't pass, document what's failing and why.

---

## Known Failure Modes from Production Testing

These are specific issues observed when running the model through the actual termfix Go binary. Your training data MUST address all of them:

1. **Hallucinated multi-line commands**: Model generates `df -h\n\nThis command shows disk usage...` inside the command parameter. Train with examples where the command is always a single clean line.

2. **Linux commands on macOS**: Model suggests `ip addr show` when platform is darwin. Train with platform-aware examples.

3. **Wrong file paths**: Model generates `/etc/_hosts` instead of `/etc/hosts`. Train with correct paths.

4. **Analysis text inside tool call parameters**: Model puts diagnostic text inside `<parameter>` values. Train with examples where tool call content is empty string and parameters are minimal.

5. **Fabricated numbers in Pass 2**: Model invents percentages and sizes not in the command output. Train with responses that ONLY reference actual values from the output.

6. **Chinese character leaks**: Base Qwen model has Chinese in its vocabulary. Train with enough English-only examples to suppress.

7. **Generation loops**: Model repeats the same sentence endlessly. This is handled by production code (`truncateRepetition()`), but training should still aim for clean, non-repetitive output.

8. **Tool calls in Pass 2**: Model tries to call a tool when no tools are defined. Train with enough Pass 2 examples (no tools key) to teach it that it should just analyze, not call tools.

---

## File Structure

```
~/termfix-training/cycle9/
├── generate_data.py       # Training data generator
├── train.py               # Training script (PEFT + SFTTrainer)
├── eval.py                # 100+ test evaluation suite
├── loop.sh                # Autonomous loop orchestrator
├── data/
│   ├── raw_outputs/       # Real command outputs for grounding
│   └── train.jsonl        # Generated training data
├── output/
│   └── iter-N/            # Per-iteration outputs + GGUFs
└── eval-results/
    ├── results.json       # Latest eval results
    └── failures.jsonl     # Failed test details
```

---

## Commands Reference

```bash
# Python environment
PYTHON=~/miniconda3/envs/gptsovits/bin/python3

# Generate training data
$PYTHON generate_data.py

# Train iteration N
$PYTHON train.py N

# Evaluate a GGUF
$PYTHON eval.py /path/to/model.gguf

# llama.cpp tools
~/llama.cpp/build/bin/llama-server    # Inference server
~/llama.cpp/build/bin/llama-quantize  # GGUF quantization
python3 ~/llama.cpp/convert_hf_to_gguf.py  # HF → GGUF conversion
```

---

## Success Criteria

The loop is DONE when a GGUF model achieves ALL of:
- **100% Pass 1 accuracy** (correct tool selection on all 25+ tests)
- **95%+ Pass 1 argument quality** (clean single-line commands)
- **95%+ Pass 2 accuracy** (grounded diagnostics on all 50+ tests)
- **95%+ Knowledge accuracy** (correct answers on all 15+ tests)
- **0 Chinese character leaks** across all tests
- **0 XML tag leaks** across all tests
- **0 template placeholder leaks** across all tests
- **0 fabricated values** in Pass 2 responses
- **GGUF size 490-530 MB** (Q4_K_M of 0.8B model)

When complete, copy the final GGUF to `~/termfix-training/cycle9/output/FINAL/` and print:

```
*** CYCLE 9 COMPLETE — ALL QUALITY GATES PASSED ***
Final GGUF: <path>
Pass rate: X/Y (Z%)
Iterations: N
```
