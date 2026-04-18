#!/usr/bin/env python3
"""Termfix Cycle 14 — V2 training data generator.

Generates training examples for the 7 new recipe domains, multi-turn probing,
and remediation. Designed to augment (not replace) existing cycle3 training data.
"""

import argparse
import json
import os
import random
import sys

SYSTEM_PROMPT = """You are termfix, an offline system troubleshooting assistant. You help diagnose and fix issues on Linux, macOS, and Windows systems.

When investigating a problem:
1. Gather information using available tools (bash, view, grep, glob)
2. Analyze the evidence
3. Provide a structured diagnostic with: Summary, Root Cause, Risk Level, Evidence, Remediation, and Rollback steps

Important rules:
- Use read-only commands — never modify the system without explicit permission
- Always explain what you're doing and why
- If you're unsure, ask for clarification
- For knowledge questions, answer directly without using tools
- You may be called multiple times. When you have enough evidence, respond with text."""

TOOLS = [
    {"type": "function", "function": {"name": "bash", "description": "Run a shell command and return its output. Use for system inspection: df, ps, ss, systemctl, docker, git, lsof, openssl, etc.", "parameters": {"type": "object", "properties": {"command": {"type": "string", "description": "The bash command to execute"}}, "required": ["command"]}}},
    {"type": "function", "function": {"name": "view", "description": "Read a file and return its contents. Use for config files, logs, /etc/* files.", "parameters": {"type": "object", "properties": {"file_path": {"type": "string", "description": "Path to the file to view"}, "offset": {"type": "integer", "description": "Line number to start from"}, "limit": {"type": "integer", "description": "Number of lines to show"}}, "required": ["file_path"]}}},
    {"type": "function", "function": {"name": "grep", "description": "Search file contents for a regex pattern. Returns matching lines.", "parameters": {"type": "object", "properties": {"pattern": {"type": "string", "description": "Regex pattern to search for"}, "path": {"type": "string", "description": "File or directory to search in"}, "include": {"type": "string", "description": "Glob pattern to filter files"}}, "required": ["pattern"]}}},
    {"type": "function", "function": {"name": "glob", "description": "Find files matching a glob pattern. Returns file paths.", "parameters": {"type": "object", "properties": {"pattern": {"type": "string", "description": "The glob pattern to match"}, "path": {"type": "string", "description": "Directory to search in"}}, "required": ["pattern"]}}},
]


def tc(name, args, cid=None):
    """Make a tool call message."""
    if cid is None:
        cid = f"call_{random.randint(1000, 9999)}"
    return {
        "role": "assistant",
        "content": None,
        "tool_calls": [{"type": "function", "function": {"name": name, "arguments": args}, "id": cid}]
    }


def tr(content, cid):
    """Make a tool response message."""
    return {"role": "tool", "content": content, "tool_call_id": cid}


def asst(content):
    """Make an assistant text message."""
    return {"role": "assistant", "content": content}


def build_example(scenario, include_tools=True):
    """Build a training example from a scenario dict."""
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": scenario["query"]},
    ]
    for call_msg, response_msg in scenario.get("turns", []):
        messages.append(call_msg)
        messages.append(response_msg)
    if scenario.get("final"):
        messages.append(asst(scenario["final"]))

    example = {"messages": messages, "category": scenario.get("category", "unknown")}
    if include_tools and scenario.get("turns"):
        example["tools"] = TOOLS
    if scenario.get("expected"):
        example["expected"] = scenario["expected"]
    if scenario.get("expected_no_tool"):
        example["expected"] = {"no_tool": True}
    return example


# =============================================================================
# PERMISSION RECIPE SCENARIOS
# =============================================================================

PERMISSION_SCENARIOS = [
    {
        "query": "permission denied on /var/log/app.log",
        "turns": [
            (tc("bash", {"command": "ls -la /var/log/app.log; id"}, "c1"),
             tr("-rw-r----- 1 root adm 12345 Apr 08 10:00 /var/log/app.log\nuid=1000(deploy) gid=1000(deploy) groups=1000(deploy),27(sudo)", "c1")),
        ],
        "final": "**Summary**: Permission denied reading `/var/log/app.log`. The file is owned by root:adm with mode 640, but your user (deploy) is not in the adm group.\n\n**Root Cause**: File permissions restrict read access to root and the adm group. User deploy is not a member of adm.\n\n**Risk Level**: LOW — read access issue, no data corruption risk.\n\n**Evidence**:\n- File mode: `-rw-r-----` (640) — owner read/write, group read-only, no other access\n- Owner: root:adm\n- Current user: deploy (groups: deploy, sudo)\n\n**Remediation**:\n1. Add user to adm group: `sudo usermod -aG adm deploy`\n2. Or change file group: `sudo chgrp deploy /var/log/app.log`\n3. Log out and back in for group changes to take effect\n\n**Rollback**: Remove from group: `sudo gpasswd -d deploy adm`",
        "expected": {"tool_name": "bash", "arguments": {"command": "ls -la /var/log/app.log; id"}},
        "category": "permission",
    },
    {
        "query": "access denied writing to /tmp/myapp",
        "turns": [
            (tc("bash", {"command": "ls -la /tmp/myapp; id"}, "c1"),
             tr("drwxr-xr-x 2 root root 4096 Apr 08 /tmp/myapp\nuid=1000(user) gid=1000(user) groups=1000(user)", "c1")),
        ],
        "final": "**Summary**: Cannot write to `/tmp/myapp`. The directory is owned by root with mode 755 (no write for others).\n\n**Root Cause**: Directory owner is root with permissions drwxr-xr-x. Your user lacks write access.\n\n**Risk Level**: LOW — permission issue.\n\n**Evidence**:\n- Directory mode: drwxr-xr-x (755)\n- Owner: root:root\n- Current user: user\n\n**Remediation**:\n1. `sudo chown user:user /tmp/myapp`\n2. Or `sudo chmod 1777 /tmp/myapp` for shared write access\n\n**Rollback**: `sudo chown root:root /tmp/myapp && sudo chmod 755 /tmp/myapp`",
        "expected": {"tool_name": "bash"},
        "category": "permission",
    },
    {
        "query": "403 forbidden when accessing web directory",
        "turns": [
            (tc("bash", {"command": "ls -la /var/www/html; id"}, "c1"),
             tr("total 12\ndrwxr-x--- 2 www-data www-data 4096 Apr 08 /var/www/html\n-rw-r--r-- 1 www-data www-data 612 Apr 08 index.html\nuid=1000(user) gid=1000(user) groups=1000(user)", "c1")),
        ],
        "final": "**Summary**: 403 Forbidden — directory `/var/www/html` has mode 750, blocking access for users outside the www-data group.\n\n**Root Cause**: Directory permissions drwxr-x--- restrict access to owner (www-data) and group (www-data). The web server user or your user is not in the www-data group.\n\n**Risk Level**: LOW — access control issue.\n\n**Evidence**:\n- Directory: drwxr-x--- (750)\n- Owner: www-data:www-data\n\n**Remediation**:\n1. `sudo chmod 755 /var/www/html`\n2. Or `sudo usermod -aG www-data user`\n\n**Rollback**: `sudo chmod 750 /var/www/html`",
        "expected": {"tool_name": "bash"},
        "category": "permission",
    },
    {
        "query": "chmod 644 not helping with script execution",
        "turns": [
            (tc("bash", {"command": "ls -la ./deploy.sh; id"}, "c1"),
             tr("-rw-r--r-- 1 user user 2048 Apr 08 ./deploy.sh\nuid=1000(user) gid=1000(user)", "c1")),
        ],
        "final": "**Summary**: Script `deploy.sh` has mode 644 which lacks execute permission.\n\n**Root Cause**: File mode rw-r--r-- (644) does not include execute bit for any user.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- Mode: -rw-r--r-- (644) — no execute bit\n\n**Remediation**: `chmod +x deploy.sh` or `chmod 755 deploy.sh`\n\n**Rollback**: `chmod 644 deploy.sh`",
        "expected": {"tool_name": "bash"},
        "category": "permission",
    },
    # macOS permission
    {
        "query": "permission denied accessing ~/Library/Application Support",
        "turns": [
            (tc("bash", {"command": "ls -la ~/Library/Application\\ Support/MyApp; id"}, "c1"),
             tr("drwx------ 3 user staff 96 Apr 08 MyApp\nuid=501(user) gid=20(staff)", "c1")),
        ],
        "final": "**Summary**: Directory exists with correct ownership but mode 700. Check if the app is running as a different user or if macOS sandboxing is blocking access.\n\n**Root Cause**: Directory is owned by you with mode 700. If access is denied, macOS App Sandbox or TCC (Transparency, Consent, and Control) may be blocking the application.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- Mode: drwx------ (700) — owner only\n- Owner: user:staff (matches current user)\n\n**Remediation**:\n1. Check System Preferences > Privacy & Security > Full Disk Access\n2. Grant the application access if needed\n\n**Rollback**: Revoke access in System Preferences if not needed.",
        "expected": {"tool_name": "bash"},
        "category": "permission_macos",
    },
]

# =============================================================================
# PORT CONFLICT SCENARIOS
# =============================================================================

PORT_SCENARIOS = [
    {
        "query": "address already in use port 8080",
        "turns": [
            (tc("bash", {"command": "lsof -iTCP -sTCP:LISTEN -P -n"}, "c1"),
             tr("COMMAND   PID USER   FD  TYPE DEVICE SIZE/OFF NODE NAME\nnode    12345 user   22u IPv4 0x1234 0t0 TCP *:8080 (LISTEN)\nnginx    5678 root   6u IPv4 0x5678 0t0 TCP *:80 (LISTEN)", "c1")),
        ],
        "final": "**Summary**: Port 8080 is already in use by a Node.js process (PID 12345).\n\n**Root Cause**: A node process owned by user is already listening on port 8080.\n\n**Risk Level**: LOW — port conflict, easy to resolve.\n\n**Evidence**:\n- `lsof` shows node (PID 12345) bound to *:8080\n\n**Remediation**:\n1. Kill the process: `kill 12345`\n2. Or use a different port for your application\n3. Check what the node process is: `ps -p 12345 -o args=`\n\n**Rollback**: N/A — killing a process is final. Restart it if needed.",
        "expected": {"tool_name": "bash"},
        "category": "port_conflict",
    },
    {
        "query": "EADDRINUSE on port 3000 when starting dev server",
        "turns": [
            (tc("bash", {"command": "lsof -iTCP -sTCP:LISTEN -P -n"}, "c1"),
             tr("COMMAND   PID USER   FD  TYPE DEVICE SIZE/OFF NODE NAME\nnode    9999 user   18u IPv6 0x9999 0t0 TCP *:3000 (LISTEN)", "c1")),
        ],
        "final": "**Summary**: Port 3000 is occupied by another Node.js process (PID 9999).\n\n**Root Cause**: A previous dev server instance (node PID 9999) is still running on port 3000.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- node (PID 9999) listening on *:3000\n\n**Remediation**:\n1. `kill 9999` to stop the old process\n2. Then restart your dev server\n\n**Rollback**: N/A.",
        "expected": {"tool_name": "bash"},
        "category": "port_conflict",
    },
    {
        "query": "bind failed on port 443, can't start nginx",
        "turns": [
            (tc("bash", {"command": "ss -tlnp"}, "c1"),
             tr("State  Recv-Q Send-Q Local Address:Port  Peer Address:Port Process\nLISTEN 0      511    0.0.0.0:443  0.0.0.0:*     users:((\"apache2\",pid=1234,fd=6))\nLISTEN 0      511    0.0.0.0:80   0.0.0.0:*     users:((\"apache2\",pid=1234,fd=4))", "c1")),
        ],
        "final": "**Summary**: Port 443 is already bound by Apache2 (PID 1234), preventing nginx from starting.\n\n**Root Cause**: Apache2 is running and holding ports 80 and 443.\n\n**Risk Level**: MEDIUM — web server down.\n\n**Evidence**:\n- apache2 (PID 1234) listening on ports 80 and 443\n\n**Remediation**:\n1. `sudo systemctl stop apache2`\n2. `sudo systemctl disable apache2` if switching to nginx permanently\n3. `sudo systemctl start nginx`\n\n**Rollback**: `sudo systemctl stop nginx && sudo systemctl start apache2`",
        "expected": {"tool_name": "bash"},
        "category": "port_conflict",
    },
    {
        "query": "port 5432 is occupied, postgres won't start",
        "turns": [
            (tc("bash", {"command": "ss -tlnp"}, "c1"),
             tr("State  Recv-Q Send-Q Local Address:Port  Peer Address:Port Process\nLISTEN 0      244    127.0.0.1:5432 0.0.0.0:*     users:((\"postgres\",pid=2222,fd=5))", "c1")),
        ],
        "final": "**Summary**: Port 5432 is in use by an existing PostgreSQL instance (PID 2222).\n\n**Root Cause**: Another PostgreSQL server is already running on port 5432.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- postgres (PID 2222) listening on 127.0.0.1:5432\n\n**Remediation**:\n1. Stop the existing instance: `sudo systemctl stop postgresql`\n2. Or configure the new instance on a different port in postgresql.conf\n\n**Rollback**: `sudo systemctl start postgresql`",
        "expected": {"tool_name": "bash"},
        "category": "port_conflict",
    },
]

# =============================================================================
# SSL/TLS SCENARIOS
# =============================================================================

SSL_SCENARIOS = [
    {
        "query": "ssl certificate expired on my server",
        "turns": [
            (tc("bash", {"command": "openssl s_client -connect localhost:443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"}, "c1"),
             tr("notBefore=Jan 1 00:00:00 2024 GMT\nnotAfter=Dec 31 23:59:59 2024 GMT\nsubject=CN = myserver.example.com", "c1")),
            (tc("bash", {"command": "date -u"}, "c2"),
             tr("Wed Apr  9 12:00:00 UTC 2026", "c2")),
        ],
        "final": "**Summary**: SSL certificate for myserver.example.com expired on Dec 31, 2024 — over 15 months ago.\n\n**Root Cause**: Certificate expired and was not renewed. notAfter date is Dec 31 23:59:59 2024 GMT.\n\n**Risk Level**: HIGH — HTTPS connections will fail with certificate errors.\n\n**Evidence**:\n- Subject: CN = myserver.example.com\n- notAfter: Dec 31 23:59:59 2024 GMT\n- Current UTC: Apr 9, 2026\n- Certificate has been expired for ~15 months\n\n**Remediation**:\n1. If using Let's Encrypt: `sudo certbot renew`\n2. If manual cert: obtain new certificate from CA\n3. Reload web server: `sudo systemctl reload nginx`\n4. Set up auto-renewal: `sudo crontab -e` → `0 0 1 * * certbot renew`\n\n**Rollback**: N/A — renewal is always safe.",
        "expected": {"tool_name": "bash"},
        "category": "ssl",
    },
    {
        "query": "tls handshake failure connecting to api.internal",
        "turns": [
            (tc("bash", {"command": "openssl s_client -connect api.internal:443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"}, "c1"),
             tr("notBefore=Mar 1 00:00:00 2026 GMT\nnotAfter=Mar 1 00:00:00 2027 GMT\nsubject=CN = *.internal.corp", "c1")),
            (tc("bash", {"command": "date -u"}, "c2"),
             tr("Wed Apr  9 12:00:00 UTC 2026", "c2")),
        ],
        "final": "**Summary**: Certificate for *.internal.corp is valid (expires Mar 2027). TLS handshake failure may be due to hostname mismatch or missing CA trust.\n\n**Root Cause**: Certificate CN is *.internal.corp but connecting to api.internal — hostname doesn't match the wildcard pattern.\n\n**Risk Level**: MEDIUM.\n\n**Evidence**:\n- Certificate CN: *.internal.corp (valid until Mar 2027)\n- Connecting to: api.internal (doesn't match *.internal.corp)\n\n**Remediation**:\n1. Use the correct hostname: `api.internal.corp` instead of `api.internal`\n2. Or add api.internal as a SAN to the certificate\n3. If self-signed, add CA to trust store\n\n**Rollback**: Revert DNS/hostname changes if needed.",
        "expected": {"tool_name": "bash"},
        "category": "ssl",
    },
    {
        "query": "x509 certificate error in python requests",
        "turns": [
            (tc("bash", {"command": "openssl s_client -connect localhost:443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"}, "c1"),
             tr("notBefore=Apr 1 00:00:00 2026 GMT\nnotAfter=Jul 1 00:00:00 2026 GMT\nsubject=CN = localhost", "c1")),
            (tc("bash", {"command": "date -u"}, "c2"),
             tr("Wed Apr  9 12:00:00 UTC 2026", "c2")),
        ],
        "final": "**Summary**: Certificate is valid (expires Jul 2026) and matches localhost. The x509 error is likely due to a self-signed certificate not trusted by Python's SSL.\n\n**Root Cause**: Self-signed certificate — Python's requests library verifies certificates by default.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- Certificate: CN=localhost, valid Apr 1 - Jul 1 2026\n- Current time: Apr 9, 2026 (within validity)\n\n**Remediation**:\n1. Add CA to system trust: `sudo cp ca.pem /usr/local/share/ca-certificates/ && sudo update-ca-certificates`\n2. Or set REQUESTS_CA_BUNDLE: `export REQUESTS_CA_BUNDLE=/path/to/ca.pem`\n3. Development only: `requests.get(url, verify=False)` (not for production)\n\n**Rollback**: Remove CA from trust store if added.",
        "expected": {"tool_name": "bash"},
        "category": "ssl",
    },
]

# =============================================================================
# GIT SCENARIOS
# =============================================================================

GIT_SCENARIOS = [
    {
        "query": "merge conflict in main.go, can't finish merge",
        "turns": [
            (tc("bash", {"command": "git status"}, "c1"),
             tr("On branch feature/auth\nYou have unmerged paths.\n  (fix conflicts and run \"git commit\")\n\nUnmerged paths:\n  (use \"git add <file>...\" to mark resolution)\n\tboth modified:   src/main.go\n\tboth modified:   src/config.go", "c1")),
            (tc("bash", {"command": "git log --oneline -5"}, "c2"),
             tr("abc1234 Add auth middleware\ndef5678 Update config parsing\n111aaaa Merge branch 'main'\n222bbbb Fix login endpoint\n333cccc Initial commit", "c2")),
        ],
        "final": "**Summary**: Merge conflicts in 2 files (src/main.go, src/config.go) during merge of feature/auth.\n\n**Root Cause**: Both branches modified the same files. Automatic merge failed.\n\n**Risk Level**: MEDIUM — merge is blocked until conflicts are resolved.\n\n**Evidence**:\n- Unmerged paths: src/main.go, src/config.go\n- Branch: feature/auth merging into main\n- Recent commits: auth middleware + config parsing changes\n\n**Remediation**:\n1. Open each conflicted file and resolve `<<<<<<<` markers\n2. `git add src/main.go src/config.go`\n3. `git commit` to complete the merge\n4. Or abort: `git merge --abort` to return to pre-merge state\n\n**Rollback**: `git merge --abort`",
        "expected": {"tool_name": "bash", "arguments": {"command": "git status"}},
        "category": "git",
    },
    {
        "query": "git push rejected, non-fast-forward",
        "turns": [
            (tc("bash", {"command": "git status"}, "c1"),
             tr("On branch main\nYour branch and 'origin/main' have diverged,\nand have 2 and 3 different commits each, respectively.", "c1")),
            (tc("bash", {"command": "git log --oneline -5"}, "c2"),
             tr("aaa1111 My local change\nbbb2222 Another local fix\nccc3333 Last common commit\nddd4444 Older commit\neee5555 Even older", "c2")),
        ],
        "final": "**Summary**: Push rejected because local main and origin/main have diverged (2 local vs 3 remote commits).\n\n**Root Cause**: Someone pushed to origin/main after your last pull. Branches have diverged.\n\n**Risk Level**: LOW — no data loss, just needs rebase/merge.\n\n**Evidence**:\n- 2 local commits ahead, 3 remote commits behind\n- Diverged from common ancestor\n\n**Remediation**:\n1. Rebase: `git pull --rebase origin main` (cleaner history)\n2. Or merge: `git pull origin main` (creates merge commit)\n3. Resolve any conflicts, then `git push`\n\n**Rollback**: `git rebase --abort` if rebase goes wrong.",
        "expected": {"tool_name": "bash", "arguments": {"command": "git status"}},
        "category": "git",
    },
    {
        "query": "detached HEAD after rebase, lost my commits",
        "turns": [
            (tc("bash", {"command": "git status"}, "c1"),
             tr("HEAD detached at abc1234\nnothing to commit, working tree clean", "c1")),
            (tc("bash", {"command": "git log --oneline -5"}, "c2"),
             tr("abc1234 (HEAD) Fix critical bug\ndef5678 Add logging\n111aaaa Update deps\n222bbbb (origin/main) Release v1.2\n333cccc Hotfix", "c2")),
        ],
        "final": "**Summary**: HEAD is detached at abc1234 but your commits are intact in the reflog.\n\n**Root Cause**: Rebase left HEAD in detached state. Commits still exist, just not on a branch.\n\n**Risk Level**: LOW — commits are not lost, just need to be attached to a branch.\n\n**Evidence**:\n- HEAD detached at abc1234\n- 3 commits ahead of origin/main\n\n**Remediation**:\n1. Create a branch from current state: `git checkout -b recovery-branch`\n2. Or re-attach to main: `git checkout main && git merge abc1234`\n3. If commits are truly lost, check reflog: `git reflog`\n\n**Rollback**: `git checkout main` to abandon detached HEAD (commits stay in reflog for 90 days).",
        "expected": {"tool_name": "bash", "arguments": {"command": "git status"}},
        "category": "git",
    },
]

# =============================================================================
# CRON SCENARIOS
# =============================================================================

CRON_SCENARIOS = [
    {
        "query": "cron job not running, backup script missed",
        "turns": [
            (tc("bash", {"command": "crontab -l 2>&1"}, "c1"),
             tr("# m h  dom mon dow  command\n0 2 * * * /opt/scripts/backup.sh >> /var/log/backup.log 2>&1\n*/5 * * * * /opt/scripts/health-check.sh", "c1")),
            (tc("bash", {"command": "journalctl -u cron -n 20 --no-pager 2>/dev/null || grep -i cron /var/log/syslog 2>/dev/null | tail -20"}, "c2"),
             tr("Apr 09 02:00:01 server CRON[12345]: (root) CMD (/opt/scripts/backup.sh >> /var/log/backup.log 2>&1)\nApr 09 02:00:01 server CRON[12345]: (CRON) error (grandchild #12345 failed with exit status 126)", "c2")),
        ],
        "final": "**Summary**: Cron job runs at 2:00 AM but fails with exit status 126 (permission denied / not executable).\n\n**Root Cause**: Exit status 126 means the script `/opt/scripts/backup.sh` is not executable.\n\n**Risk Level**: MEDIUM — backups are not running.\n\n**Evidence**:\n- Crontab entry: `0 2 * * * /opt/scripts/backup.sh`\n- Cron log: exit status 126\n- 2 cron entries found total\n\n**Remediation**:\n1. Make script executable: `sudo chmod +x /opt/scripts/backup.sh`\n2. Verify script has correct shebang: `head -1 /opt/scripts/backup.sh`\n3. Test manually: `sudo /opt/scripts/backup.sh`\n\n**Rollback**: N/A — fixing permissions is safe.",
        "expected": {"tool_name": "bash"},
        "category": "cron",
    },
    {
        "query": "crontab is empty, scheduled tasks disappeared",
        "turns": [
            (tc("bash", {"command": "crontab -l 2>&1"}, "c1"),
             tr("no crontab for user", "c1")),
        ],
        "final": "**Summary**: No crontab entries found for current user.\n\n**Root Cause**: Crontab is empty — entries may have been accidentally deleted or never set up for this user.\n\n**Risk Level**: MEDIUM — scheduled tasks are not running.\n\n**Evidence**:\n- `crontab -l` shows no crontab\n- 0 cron entries found\n\n**Remediation**:\n1. Check if root has the crontab: `sudo crontab -l`\n2. Check system-wide cron: `ls /etc/cron.d/`\n3. Restore from backup if available\n4. Re-create entries with `crontab -e`\n\n**Rollback**: Previous crontab may be recoverable from `/var/spool/cron/crontabs/` backups.",
        "expected": {"tool_name": "bash"},
        "category": "cron",
    },
    {
        "query": "systemd timer not firing as expected",
        "turns": [
            (tc("bash", {"command": "crontab -l 2>&1"}, "c1"),
             tr("no crontab for user", "c1")),
            (tc("bash", {"command": "systemctl list-timers --no-pager"}, "c2"),
             tr("NEXT                         LEFT          LAST                         PASSED       UNIT                         ACTIVATES\nThu 2026-04-10 00:00:00 UTC  11h left      Wed 2026-04-09 00:00:00 UTC  12h ago      backup.timer                 backup.service\n\n1 timers listed.", "c2")),
        ],
        "final": "**Summary**: No crontab entries, but a systemd timer (backup.timer) is configured and appears active.\n\n**Root Cause**: Tasks are managed by systemd timers, not cron. Timer last fired 12 hours ago and next fires in 11 hours.\n\n**Risk Level**: LOW — timer appears to be functioning normally.\n\n**Evidence**:\n- No crontab (using systemd timers instead)\n- backup.timer: last ran 12h ago, next in 11h\n\n**Remediation**:\n1. Check service status: `systemctl status backup.service`\n2. Check timer logs: `journalctl -u backup.service -n 20`\n3. Trigger manually: `sudo systemctl start backup.service`\n\n**Rollback**: N/A.",
        "expected": {"tool_name": "bash"},
        "category": "cron",
    },
]

# =============================================================================
# PROCESS SCENARIOS
# =============================================================================

PROCESS_SCENARIOS = [
    {
        "query": "zombie processes piling up",
        "turns": [
            (tc("bash", {"command": "ps aux | grep -E 'Z|defunct'"}, "c1"),
             tr("root     1234  0.0  0.0      0     0 ?        Z    Apr08   0:00 [worker] <defunct>\nroot     1235  0.0  0.0      0     0 ?        Z    Apr08   0:00 [worker] <defunct>\nroot     1236  0.0  0.0      0     0 ?        Z    Apr08   0:00 [worker] <defunct>\nuser     9999  0.0  0.0  12345  1234 pts/0    S+   12:00   0:00 grep defunct", "c1")),
            (tc("bash", {"command": "lsof | awk '{print $1}' | sort | uniq -c | sort -rn | head"}, "c2"),
             tr("   245 chrome\n   128 node\n    56 python3\n    34 bash\n    23 sshd\n    12 nginx", "c2")),
        ],
        "final": "**Summary**: 3 zombie (defunct) processes detected, all children of a worker process.\n\n**Root Cause**: Parent process is not properly reaping child processes (not calling wait()).\n\n**Risk Level**: LOW — zombies consume no resources but indicate a programming bug.\n\n**Evidence**:\n- 3 zombie processes: PIDs 1234, 1235, 1236 (all [worker] <defunct>)\n- Top file descriptor users: chrome (245), node (128)\n\n**Remediation**:\n1. Find parent: `ps -o ppid= -p 1234`\n2. Restart parent process to clear zombies\n3. Fix the application to properly handle SIGCHLD\n\n**Rollback**: N/A — restarting the parent clears zombies.",
        "expected": {"tool_name": "bash"},
        "category": "process",
    },
    {
        "query": "too many open files error in my application",
        "turns": [
            (tc("bash", {"command": "ulimit -n"}, "c1"),
             tr("1024", "c1")),
            (tc("bash", {"command": "lsof | awk '{print $1}' | sort | uniq -c | sort -rn | head"}, "c2"),
             tr("  1018 myapp\n   245 chrome\n   128 node\n    56 python3", "c2")),
        ],
        "final": "**Summary**: Application `myapp` has 1018 open file descriptors, approaching the limit of 1024.\n\n**Root Cause**: ulimit is set to 1024 and myapp has nearly exhausted it with 1018 open files. This triggers EMFILE errors.\n\n**Risk Level**: HIGH — application will fail to open any new files or connections.\n\n**Evidence**:\n- ulimit -n: 1024\n- myapp: 1018 open file descriptors (99.4% of limit)\n\n**Remediation**:\n1. Increase limit temporarily: `ulimit -n 65536`\n2. Permanent: add to `/etc/security/limits.conf`:\n   `user soft nofile 65536`\n   `user hard nofile 65536`\n3. Investigate if myapp has a file descriptor leak\n\n**Rollback**: Revert limits.conf changes and restart the service.",
        "expected": {"tool_name": "bash"},
        "category": "process",
    },
]

# =============================================================================
# PACKAGE MANAGER SCENARIOS
# =============================================================================

PACKAGE_SCENARIOS = [
    {
        "query": "dpkg was interrupted, apt is broken",
        "turns": [
            (tc("bash", {"command": "apt list --upgradable 2>&1 | head -20"}, "c1"),
             tr("E: dpkg was interrupted, you must manually run 'sudo dpkg --configure -a' to correct the problem.\nE: _cache->open() failed, please report.", "c1")),
        ],
        "final": "**Summary**: dpkg was interrupted during a previous operation, leaving the package database in an inconsistent state.\n\n**Root Cause**: A previous apt/dpkg operation was interrupted (system crash, Ctrl+C, or power loss).\n\n**Risk Level**: HIGH — package manager is non-functional.\n\n**Evidence**:\n- Error: dpkg was interrupted\n- apt cache cannot open\n\n**Remediation**:\n1. `sudo dpkg --configure -a`\n2. `sudo apt-get install -f`\n3. `sudo apt-get update`\n\n**Rollback**: N/A — these commands repair the package database.",
        "expected": {"tool_name": "bash"},
        "category": "package",
    },
    {
        "query": "brew install fails with locked error on mac",
        "turns": [
            (tc("bash", {"command": "brew doctor 2>&1 | head -30"}, "c1"),
             tr("Please note that these warnings are just used to help the Homebrew maintainers\nwith debugging if you file an issue. If everything you use Homebrew for is\nworking fine: please don't worry or file an issue; just ignore this. Thanks!\n\nWarning: You have unlinked kegs in your Cellar.\nLinking new versions of formulae may fail with:\n  Error: Could not symlink ...\n\nTry to link them with: brew link <formula>", "c1")),
        ],
        "final": "**Summary**: Homebrew has unlinked kegs causing installation failures.\n\n**Root Cause**: Some previously installed formulas have unlinked kegs, preventing new versions from creating symlinks.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- `brew doctor` warns about unlinked kegs\n\n**Remediation**:\n1. `brew cleanup`\n2. `brew link --overwrite <formula>` for each unlinked formula\n3. If permissions: `sudo chown -R $(whoami) $(brew --prefix)/*`\n\n**Rollback**: `brew unlink <formula>` if needed.",
        "expected": {"tool_name": "bash"},
        "category": "package_macos",
    },
    {
        "query": "pip install giving dependency errors",
        "turns": [
            (tc("bash", {"command": "pip3 list --outdated 2>&1 | head -20"}, "c1"),
             tr("Package    Version Latest Type\n---------- ------- ------ -----\nnumpy      1.21.0  1.26.4 wheel\npandas     1.3.0   2.2.1  wheel\nrequests   2.25.0  2.31.0 wheel\nWARNING: pip is configured with locations that require TLS/SSL, however the ssl module in Python is not available.", "c1")),
        ],
        "final": "**Summary**: Python SSL module is not available, causing pip TLS failures.\n\n**Root Cause**: Python was compiled without SSL support or openssl libraries are missing.\n\n**Risk Level**: MEDIUM — pip cannot securely download packages.\n\n**Evidence**:\n- WARNING: ssl module not available\n- 3 outdated packages detected\n\n**Remediation**:\n1. Install SSL development headers: `sudo apt install libssl-dev`\n2. Rebuild Python or use system Python\n3. Or use `--trusted-host pypi.org --trusted-host pypi.python.org` temporarily\n\n**Rollback**: N/A.",
        "expected": {"tool_name": "bash"},
        "category": "package",
    },
]

# =============================================================================
# MULTI-TURN CHAIN SCENARIOS (2-3 probes before diagnosis)
# =============================================================================

MULTI_TURN_SCENARIOS = [
    {
        "query": "my web app is returning 502 errors",
        "turns": [
            (tc("bash", {"command": "systemctl status nginx --no-pager"}, "c1"),
             tr("● nginx.service - A high performance web server\n   Active: active (running) since Mon 2026-04-07\n   Main PID: 1234 (nginx)", "c1")),
            (tc("bash", {"command": "journalctl -u nginx -n 20 --no-pager"}, "c2"),
             tr("Apr 09 12:00:01 web01 nginx[1234]: *1 connect() failed (111: Connection refused) while connecting to upstream, client: 10.0.0.1, server: app.example.com, upstream: \"http://127.0.0.1:3000/\"", "c2")),
            (tc("bash", {"command": "ss -tlnp | grep 3000"}, "c3"),
             tr("", "c3")),
        ],
        "final": "**Summary**: nginx is returning 502 because the upstream application on port 3000 is not running.\n\n**Root Cause**: nginx is proxying to 127.0.0.1:3000 but nothing is listening on that port. The backend application has stopped.\n\n**Risk Level**: HIGH — all requests are failing with 502.\n\n**Evidence**:\n- nginx is running (active)\n- nginx logs: connect() failed (111: Connection refused) to upstream :3000\n- Port 3000 has no listener\n\n**Remediation**:\n1. Check backend service: `systemctl status myapp`\n2. Start it: `sudo systemctl start myapp`\n3. Verify port 3000 comes up: `ss -tlnp | grep 3000`\n\n**Rollback**: N/A.",
        "expected": {"tool_name": "bash"},
        "category": "multi_turn",
    },
    {
        "query": "database connection timeouts in production",
        "turns": [
            (tc("bash", {"command": "systemctl status postgresql --no-pager"}, "c1"),
             tr("● postgresql.service - PostgreSQL RDBMS\n   Active: active (running)\n   Main PID: 5678 (postgres)", "c1")),
            (tc("bash", {"command": "ss -tlnp | grep 5432"}, "c2"),
             tr("LISTEN 0 244 127.0.0.1:5432 0.0.0.0:* users:((\"postgres\",pid=5678,fd=5))", "c2")),
            (tc("bash", {"command": "df -h"}, "c3"),
             tr("Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1       100G   98G  2.0G  98% /\ntmpfs           7.8G     0  7.8G   0% /dev/shm", "c3")),
        ],
        "final": "**Summary**: PostgreSQL is running but the root filesystem is 98% full, which can cause database write failures and connection timeouts.\n\n**Root Cause**: Disk at 98% usage (2GB free). PostgreSQL may be unable to write WAL or temp files, causing queries to hang and connections to time out.\n\n**Risk Level**: CRITICAL — database may crash if disk fills completely.\n\n**Evidence**:\n- PostgreSQL: active on port 5432\n- Disk: /dev/sda1 at 98% (only 2GB free)\n\n**Remediation**:\n1. Free space immediately: `sudo journalctl --vacuum-size=500M`\n2. Clean old logs: `sudo find /var/log -name '*.gz' -mtime +30 -delete`\n3. Check PostgreSQL WAL: `du -sh /var/lib/postgresql/*/main/pg_wal/`\n4. Long term: add disk space or move data directory\n\n**Rollback**: N/A — freeing disk space is always safe.",
        "expected": {"tool_name": "bash"},
        "category": "multi_turn",
    },
    {
        "query": "kubernetes pod keeps crashing with OOMKilled",
        "turns": [
            (tc("bash", {"command": "free -h"}, "c1"),
             tr("              total        used        free      shared  buff/cache   available\nMem:           15Gi       14Gi       256Mi       128Mi       1.2Gi       800Mi\nSwap:         4.0Gi       3.8Gi       200Mi", "c1")),
            (tc("bash", {"command": "ps aux --sort=-%mem | head -10"}, "c2"),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\nroot     12345 15.0 45.2 8912345 7200000 ?   Sl   Apr08 12:34 java -Xmx8g -jar /app.jar\nroot     23456  5.0 20.1 3456789 3200000 ?   Sl   Apr08  5:00 node /frontend/server.js\nroot     34567  2.0  8.5 1234567 1350000 ?   Ssl  Apr08  3:00 mongod --dbpath /data", "c2")),
        ],
        "final": "**Summary**: System is critically low on memory (256MB free, 3.8GB swap used). A Java process using 45% of RAM is the primary consumer.\n\n**Root Cause**: Java application with -Xmx8g is consuming 6.8GB on a 15GB system, leaving insufficient memory for other processes. OOM killer is triggered.\n\n**Risk Level**: CRITICAL — system is in swap thrash, OOMKilled events expected.\n\n**Evidence**:\n- Free RAM: 256MB (of 15GB)\n- Swap: 95% used (3.8G/4.0G)\n- java (PID 12345): 45.2% mem, 6.8GB RSS, -Xmx8g\n- node (PID 23456): 20.1% mem, 3.0GB RSS\n\n**Remediation**:\n1. Reduce Java heap: change -Xmx8g to -Xmx4g\n2. Set container memory limits\n3. Restart the Java process: `kill -TERM 12345`\n4. Monitor: `watch free -h`\n\n**Rollback**: Revert -Xmx change and restart.",
        "expected": {"tool_name": "bash"},
        "category": "multi_turn",
    },
]

# =============================================================================
# REMEDIATION TRAINING SCENARIOS
# =============================================================================

REMEDIATION_SCENARIOS = [
    {
        "query": "disk full, need to free space",
        "turns": [
            (tc("bash", {"command": "df -h"}, "c1"),
             tr("Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   48G  2.0G  96% /", "c1")),
        ],
        "final": "**Summary**: Root filesystem at 96% (48G/50G).\n\n**Remediation**:\n1. Clean journal logs: `sudo journalctl --vacuum-size=500M`\n2. Remove old packages: `sudo apt autoremove`\n3. Find large files: `sudo find / -xdev -size +100M -exec ls -lh {} \\;`",
        "expected": {"tool_name": "bash", "arguments": {"command": "df -h"}},
        "category": "remediation",
    },
    {
        "query": "nginx won't start, fix it",
        "turns": [
            (tc("bash", {"command": "systemctl status nginx --no-pager --full -n 20"}, "c1"),
             tr("● nginx.service - A high performance web server\n   Active: failed (Result: exit-code)\n  Process: 1234 ExecStart=/usr/sbin/nginx (code=exited, status=1/FAILURE)\nApr 09 nginx[1234]: nginx: [emerg] unknown directive \"proxy_passs\" in /etc/nginx/sites-enabled/default:12", "c1")),
        ],
        "final": "**Summary**: nginx fails to start due to a typo in config — `proxy_passs` (should be `proxy_pass`).\n\n**Root Cause**: Configuration syntax error at /etc/nginx/sites-enabled/default:12.\n\n**Risk Level**: MEDIUM.\n\n**Remediation**:\n1. Fix typo: `sudo sed -i 's/proxy_passs/proxy_pass/' /etc/nginx/sites-enabled/default`\n2. Test config: `sudo nginx -t`\n3. Start: `sudo systemctl start nginx`\n\n**Rollback**: `sudo systemctl stop nginx` and revert the config change.",
        "expected": {"tool_name": "bash"},
        "category": "remediation",
    },
    {
        "query": "redis service keeps restarting",
        "turns": [
            (tc("bash", {"command": "systemctl status redis --no-pager --full -n 20"}, "c1"),
             tr("● redis-server.service - Advanced key-value store\n   Active: activating (auto-restart)\n   Main PID: 5678 (code=exited, status=1/FAILURE)\nApr 09 redis[5678]: Can't open the log file: Permission denied", "c1")),
        ],
        "final": "**Summary**: Redis keeps crashing because it can't write to its log file (permission denied).\n\n**Root Cause**: Redis log file or directory has incorrect permissions.\n\n**Risk Level**: MEDIUM.\n\n**Remediation**:\n1. Fix log permissions: `sudo chown redis:redis /var/log/redis/`\n2. `sudo chmod 750 /var/log/redis/`\n3. Restart: `sudo systemctl restart redis`\n\n**Rollback**: `sudo systemctl stop redis` and revert permissions.",
        "expected": {"tool_name": "bash"},
        "category": "remediation",
    },
]

# =============================================================================
# KNOWLEDGE Q&A (no tools) SCENARIOS
# =============================================================================

KNOWLEDGE_SCENARIOS = [
    {
        "query": "what is a zombie process",
        "turns": [],
        "final": "A zombie process is a process that has completed execution but still has an entry in the process table. This happens when the parent process hasn't read the child's exit status using wait(). Zombies show as 'Z' state in `ps` output. They consume no CPU or memory but occupy a process table slot. To clear them, the parent process must call wait(), or you can kill the parent process.",
        "expected_no_tool": True,
        "category": "knowledge",
    },
    {
        "query": "explain SSL certificate chain of trust",
        "turns": [],
        "final": "An SSL certificate chain of trust works as follows: Your server's certificate is signed by an intermediate Certificate Authority (CA), which is in turn signed by a root CA. Browsers and operating systems ship with a set of trusted root CA certificates. When connecting, the server presents its certificate plus any intermediate certificates. The client verifies each signature up the chain until it reaches a trusted root. If any link is missing or expired, the connection fails with a certificate error.",
        "expected_no_tool": True,
        "category": "knowledge",
    },
    {
        "query": "what does crontab syntax mean",
        "turns": [],
        "final": "Crontab uses 5 time fields followed by a command: `minute hour day-of-month month day-of-week command`. For example, `*/5 * * * *` means every 5 minutes. `0 2 * * *` means daily at 2:00 AM. `0 0 1 * *` means the first of every month at midnight. Special characters: `*` (any), `,` (list), `-` (range), `/` (step). Use `crontab -e` to edit and `crontab -l` to list entries.",
        "expected_no_tool": True,
        "category": "knowledge",
    },
    {
        "query": "how does git rebase work",
        "turns": [],
        "final": "Git rebase replays your branch's commits on top of another branch. When you run `git rebase main`, git: 1) Finds the common ancestor of your branch and main, 2) Saves your commits since that point, 3) Resets your branch to main's tip, 4) Re-applies your commits one by one. This creates a linear history without merge commits. Use `--interactive` for squashing/reordering. Caution: never rebase published/shared branches as it rewrites commit hashes.",
        "expected_no_tool": True,
        "category": "knowledge",
    },
    {
        "query": "what is the difference between TCP and UDP",
        "turns": [],
        "final": "TCP (Transmission Control Protocol) is connection-oriented, reliable, and ordered — it guarantees delivery via acknowledgments and retransmissions. UDP (User Datagram Protocol) is connectionless, unreliable, and unordered — packets may be lost or arrive out of order. TCP is used for HTTP, SSH, databases. UDP is used for DNS, streaming, gaming, and VoIP where speed matters more than reliability. TCP has higher overhead due to handshakes and acknowledgments.",
        "expected_no_tool": True,
        "category": "knowledge",
    },
]

# =============================================================================
# NEGATIVE / SAFETY SCENARIOS
# =============================================================================

NEGATIVE_SCENARIOS = [
    {
        "query": "delete all files on the server",
        "turns": [],
        "final": "I cannot execute destructive commands like `rm -rf /`. That would permanently destroy all data on the system. If you need to free disk space, I can help identify large files safely with `du -sh /* | sort -rh` and suggest targeted cleanup.",
        "expected_no_tool": True,
        "category": "negative",
    },
    {
        "query": "show me /etc/shadow contents",
        "turns": [],
        "final": "I cannot access `/etc/shadow` as it contains sensitive password hashes. If you need to check user accounts, I can look at `/etc/passwd` which contains non-sensitive user information, or check authentication logs with `journalctl -u sshd`.",
        "expected_no_tool": True,
        "category": "negative",
    },
    {
        "query": "run rm -rf /* to clean up the system",
        "turns": [],
        "final": "That command would destroy the entire filesystem and is extremely dangerous. I will not execute it. For safe disk cleanup, try:\n1. `sudo apt autoremove` to remove unused packages\n2. `sudo journalctl --vacuum-size=500M` to trim logs\n3. `du -sh /var/* | sort -rh` to find space consumers",
        "expected_no_tool": True,
        "category": "negative",
    },
]

# =============================================================================
# macOS-SPECIFIC SCENARIOS
# =============================================================================

MACOS_SCENARIOS = [
    {
        "query": "launchctl service not loading on mac",
        "turns": [
            (tc("bash", {"command": "launchctl list | grep myapp"}, "c1"),
             tr("-\t78\tcom.myapp.agent", "c1")),
        ],
        "final": "**Summary**: Service com.myapp.agent exited with status 78 (configuration error).\n\n**Root Cause**: Exit code 78 indicates the plist configuration is invalid.\n\n**Risk Level**: LOW.\n\n**Evidence**:\n- launchctl shows exit code 78 for com.myapp.agent\n\n**Remediation**:\n1. Check plist syntax: `plutil ~/Library/LaunchAgents/com.myapp.agent.plist`\n2. Check logs: `log show --predicate 'subsystem==\"com.myapp\"' --last 5m`\n3. Reload: `launchctl unload ~/Library/LaunchAgents/com.myapp.agent.plist && launchctl load ~/Library/LaunchAgents/com.myapp.agent.plist`\n\n**Rollback**: `launchctl unload ~/Library/LaunchAgents/com.myapp.agent.plist`",
        "expected": {"tool_name": "bash"},
        "category": "macos_service",
    },
    {
        "query": "high memory pressure on macOS, beach balling",
        "turns": [
            (tc("bash", {"command": "vm_stat"}, "c1"),
             tr("Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:                               2048.\nPages active:                           524288.\nPages inactive:                          65536.\nPages speculative:                        1024.\nPages throttled:                             0.\nPages wired down:                       262144.\nPages purgeable:                          4096.\nPages stored in compressor:             131072.\nPages occupied by compressor:            32768.", "c1")),
        ],
        "final": "**Summary**: macOS memory pressure is high. Only 32MB free pages, with 131K pages in compressor (2GB compressed).\n\n**Root Cause**: Physical memory is nearly exhausted. The compressor is handling 2GB of compressed data, indicating heavy memory pressure.\n\n**Risk Level**: HIGH — beach balling and app slowdowns expected.\n\n**Evidence**:\n- Free pages: 2048 (32MB at 16K page size)\n- Active: 524288 pages (8GB)\n- Wired: 262144 pages (4GB)\n- Compressor: 131072 pages stored (2GB compressed)\n\n**Remediation**:\n1. Close unused applications\n2. Check Activity Monitor for top memory consumers\n3. Restart memory-heavy apps (browsers, IDEs)\n\n**Rollback**: N/A.",
        "expected": {"tool_name": "bash"},
        "category": "macos_memory",
    },
]

# =============================================================================
# COMBINE ALL SCENARIOS
# =============================================================================

ALL_SCENARIOS = (
    PERMISSION_SCENARIOS
    + PORT_SCENARIOS
    + SSL_SCENARIOS
    + GIT_SCENARIOS
    + CRON_SCENARIOS
    + PROCESS_SCENARIOS
    + PACKAGE_SCENARIOS
    + MULTI_TURN_SCENARIOS
    + REMEDIATION_SCENARIOS
    + KNOWLEDGE_SCENARIOS
    + NEGATIVE_SCENARIOS
    + MACOS_SCENARIOS
)


def generate_variations(scenario, count=3):
    """Generate query variations of a scenario."""
    prefixes = ["", "Hey, ", "Help me: ", "Can you check ", "I'm having trouble. ", "Urgent: "]
    suffixes = ["", " Thanks.", " Any ideas?", " What should I do?", " Can you help?"]
    variations = []
    used = {scenario["query"]}
    for _ in range(count * 2):
        q = f"{random.choice(prefixes)}{scenario['query']}{random.choice(suffixes)}".strip()
        if q not in used:
            used.add(q)
            v = dict(scenario)
            v["query"] = q
            variations.append(v)
        if len(variations) >= count:
            break
    return variations


def main():
    parser = argparse.ArgumentParser(description="Generate v2 training data")
    parser.add_argument("--output-dir", default="data", help="Output directory")
    parser.add_argument("--variations", type=int, default=4, help="Variations per scenario")
    parser.add_argument("--seed", type=int, default=42, help="Random seed")
    parser.add_argument("--merge-with", default=None, help="Path to existing train.jsonl to merge with")
    args = parser.parse_args()

    random.seed(args.seed)
    os.makedirs(args.output_dir, exist_ok=True)

    print(f"=== Termfix V2 Data Generator ===")
    print(f"Base scenarios: {len(ALL_SCENARIOS)}")

    # Build examples with variations
    all_examples = []
    for scenario in ALL_SCENARIOS:
        include_tools = bool(scenario.get("turns"))
        all_examples.append(build_example(scenario, include_tools))
        for var in generate_variations(scenario, count=args.variations):
            all_examples.append(build_example(var, include_tools))

    print(f"Generated {len(all_examples)} examples (with variations)")

    # Category distribution
    cat_counts = {}
    for ex in all_examples:
        cat = ex.get("category", "unknown")
        cat_counts[cat] = cat_counts.get(cat, 0) + 1
    print("\nCategory distribution:")
    for cat, count in sorted(cat_counts.items(), key=lambda x: -x[1]):
        print(f"  {cat}: {count}")

    # Merge with existing data if specified
    if args.merge_with and os.path.exists(args.merge_with):
        existing = []
        with open(args.merge_with) as f:
            for line in f:
                line = line.strip()
                if line:
                    existing.append(json.loads(line))
        print(f"\nMerging with {len(existing)} existing examples from {args.merge_with}")
        all_examples = existing + all_examples

    # Shuffle
    random.shuffle(all_examples)

    # Split: 90% train, 5% eval, 5% valid
    n = len(all_examples)
    eval_n = max(50, n // 20)
    valid_n = max(50, n // 20)
    train_n = n - eval_n - valid_n

    train_examples = all_examples[:train_n]
    eval_examples = all_examples[train_n:train_n + eval_n]
    valid_examples = all_examples[train_n + eval_n:]

    # Write
    for name, examples in [("train", train_examples), ("eval", eval_examples), ("valid", valid_examples)]:
        path = os.path.join(args.output_dir, f"{name}.jsonl")
        with open(path, "w") as f:
            for ex in examples:
                f.write(json.dumps(ex) + "\n")
        print(f"  {name}: {len(examples)} → {path}")

    print(f"\nTotal: {n} examples")


if __name__ == "__main__":
    main()
