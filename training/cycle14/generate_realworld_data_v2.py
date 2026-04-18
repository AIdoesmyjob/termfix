#!/usr/bin/env python3
"""Termfix Cycle 14v4 — Round 2 targeted training for remaining 8 real-world failures.

Each failure gets 3-5 examples with variations to reinforce the correct pattern.
"""

import json
import os
import random

SYSTEM_PROMPT = """You are termfix, an offline system troubleshooting assistant running in a terminal.

Your job in this step is only to decide the single best next probe.

You diagnose system issues using read-only inspection tools: bash (for running commands), file viewer, glob, and grep.
You CANNOT modify files — only inspect and diagnose.

Rules:
- Either answer directly for simple knowledge questions, or make a tool call to gather evidence
- Prefer the smallest probe that will reduce uncertainty the most
- You may be called multiple times. When you have enough evidence, respond with text.
- Do not explain the final diagnosis yet
- Keep any text response short and direct

Tool guide:
- bash: system commands (df, ps, ss, systemctl, docker, git status, lsof, openssl)
- view: read a known file (/etc/hosts, config files, logs)
- grep: search patterns in files (errors in logs, config values)
- glob: find files by name (*.log, *.conf, core dumps)"""

TOOLS = [
    {"type": "function", "function": {"name": "bash", "description": "Run a shell command and return its output. Use for system inspection: df, ps, ss, systemctl, docker, git, lsof, openssl, etc.", "parameters": {"type": "object", "properties": {"command": {"type": "string", "description": "The bash command to execute"}}, "required": ["command"]}}},
    {"type": "function", "function": {"name": "view", "description": "Read a file and return its contents. Use for config files, logs, /etc/* files.", "parameters": {"type": "object", "properties": {"file_path": {"type": "string", "description": "Path to the file to view"}, "offset": {"type": "integer", "description": "Line number to start from"}, "limit": {"type": "integer", "description": "Number of lines to show"}}, "required": ["file_path"]}}},
    {"type": "function", "function": {"name": "grep", "description": "Search file contents for a regex pattern. Returns matching lines.", "parameters": {"type": "object", "properties": {"pattern": {"type": "string", "description": "Regex pattern to search for"}, "path": {"type": "string", "description": "File or directory to search in"}, "include": {"type": "string", "description": "Glob pattern to filter files"}}, "required": ["pattern"]}}},
    {"type": "function", "function": {"name": "glob", "description": "Find files matching a glob pattern. Returns file paths.", "parameters": {"type": "object", "properties": {"pattern": {"type": "string", "description": "The glob pattern to match"}, "path": {"type": "string", "description": "Directory to search in"}}, "required": ["pattern"]}}},
]


def tc(name, args, cid=None):
    if cid is None:
        cid = f"call_{random.randint(1000, 9999)}"
    return {
        "role": "assistant",
        "content": None,
        "tool_calls": [{"type": "function", "function": {"name": name, "arguments": args}, "id": cid}]
    }


def tr(content, cid):
    return {"role": "tool", "content": content, "tool_call_id": cid}


def asst(content):
    return {"role": "assistant", "content": content}


def build_example(query, turns, final_text, category="general"):
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": query},
    ]
    for call_msg, resp_msg in turns:
        messages.append(call_msg)
        messages.append(resp_msg)
    messages.append(asst(final_text))
    return {"messages": messages, "tools": TOOLS, "category": category}


def generate_scenarios():
    scenarios = []

    # =========================================================================
    # GAP 1: INODE — model must learn "can't create files + space available" → df -i
    # The key signal is: "space available" or "df shows space" or "not full" + "can't create"
    # =========================================================================

    c1 = "call_101"
    c2 = "call_102"
    scenarios.append(build_example(
        "I can't create new files even though df shows space available. 'No space left on device' error.",
        [
            (tc("bash", {"command": "df -i"}, c1),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1      6553600 6553600       0  100% /\ntmpfs          2048000      10 2047990    1% /dev/shm", c1)),
        ],
        "**Summary**: All inodes exhausted on / — 0 free inodes despite available disk space.\n\n**Root Cause**: /dev/sda1 has 0 free inodes (100% IUse). The filesystem can store more data but cannot create new directory entries. This happens when millions of tiny files consume all inode slots.\n\n**Risk Level**: High — no new files can be created.\n\n**Evidence**: `df -i` shows 0 IFree on /.\n\n**Remediation**:\n1. Find directories with many files: `find / -xdev -type d -exec sh -c 'echo \"$(ls -1 \"$1\" 2>/dev/null | wc -l) $1\"' _ {} \\; 2>/dev/null | sort -rn | head -10`\n2. Remove unnecessary files from the top directory\n3. Verify: `df -i`\n\n**Rollback**: Only remove files confirmed as unnecessary.",
        "disk"
    ))

    c1 = "call_103"
    scenarios.append(build_example(
        "Getting 'No space left on device' but disk usage shows only 40%. What's wrong?",
        [
            (tc("bash", {"command": "df -i"}, c1),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1      1310720 1310720       0  100% /", c1)),
        ],
        "**Summary**: Inodes exhausted — 100% used despite 60% disk space free.\n\n**Root Cause**: All 1,310,720 inodes consumed on /dev/sda1. Many small files exist somewhere.\n\n**Risk Level**: High\n\n**Evidence**: `df -i` shows 0 free inodes.\n\n**Remediation**: Find and remove the directory with excessive small files.",
        "disk"
    ))

    c1 = "call_104"
    scenarios.append(build_example(
        "touch: cannot touch 'test.txt': No space left on device. But df says 28G available!",
        [
            (tc("bash", {"command": "df -i"}, c1),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1      3276800 3276800       0  100% /\ntmpfs          1024000      10 1023990    1% /dev/shm", c1)),
        ],
        "**Summary**: Disk space is fine but inodes are fully consumed.\n\n**Root Cause**: All 3,276,800 inodes used on /. `touch` fails because creating a file requires a free inode, regardless of disk space.\n\n**Risk Level**: High\n\n**Evidence**: `df -i` shows 100% inode usage on /.",
        "disk"
    ))

    c1 = "call_105"
    c2 = "call_106"
    scenarios.append(build_example(
        "No space left on device error when writing to /tmp. df -h shows the disk isn't full.",
        [
            (tc("bash", {"command": "df -i /tmp"}, c1),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1      524288  524288       0  100% /", c1)),
        ],
        "**Summary**: Inodes exhausted on the partition containing /tmp.\n\n**Root Cause**: 0 free inodes. When disk space is available but files can't be created, inode exhaustion is the cause.\n\n**Risk Level**: High\n\n**Evidence**: `df -i` shows 100% inode usage.",
        "disk"
    ))

    # Also teach: when user says "disk full" without the "space available" signal, df -h is correct first
    c1 = "call_107"
    scenarios.append(build_example(
        "The disk is full. What's using all the space?",
        [
            (tc("bash", {"command": "df -h"}, c1),
             tr("Filesystem      Size  Used Avail Use%  Mounted on\n/dev/sda1       100G   95G   5G   95% /", c1)),
        ],
        "**Summary**: Root partition at 95% — 5G remaining.\n\n**Risk Level**: High\n\n**Evidence**: `df -h` shows / at 95%.\n\n**Remediation**: Find large directories: `du -h --max-depth=1 / 2>/dev/null | sort -rh | head -15`",
        "disk"
    ))

    # =========================================================================
    # GAP 2: DNS — when dig fails/times out, check /etc/resolv.conf
    # =========================================================================

    c1 = "call_201"
    c2 = "call_202"
    scenarios.append(build_example(
        "I can't resolve any domain names. ping google.com says 'Temporary failure in name resolution'.",
        [
            (tc("bash", {"command": "dig google.com"}, c1),
             tr(";; connection timed out; no servers could be reached", c1)),
            (tc("view", {"file_path": "/etc/resolv.conf"}, c2),
             tr("nameserver 192.0.2.1", c2)),
        ],
        "**Summary**: DNS resolution failing — nameserver 192.0.2.1 is unreachable.\n\n**Root Cause**: /etc/resolv.conf points to 192.0.2.1 which is not responding (dig timed out). This is a TEST-NET address — not a valid DNS server.\n\n**Risk Level**: High\n\n**Evidence**: `dig` timed out. /etc/resolv.conf has `nameserver 192.0.2.1`.\n\n**Remediation**:\n1. Replace nameserver: `sudo sh -c 'echo \"nameserver 8.8.8.8\" > /etc/resolv.conf'`\n2. Test: `dig google.com`\n\n**Rollback**: Restore original resolv.conf.",
        "dns"
    ))

    c1 = "call_203"
    c2 = "call_204"
    scenarios.append(build_example(
        "DNS stopped working suddenly. No domains resolve.",
        [
            (tc("bash", {"command": "dig example.com"}, c1),
             tr(";; connection timed out; no servers could be reached", c1)),
            (tc("view", {"file_path": "/etc/resolv.conf"}, c2),
             tr("# This file is empty", c2)),
        ],
        "**Summary**: /etc/resolv.conf is empty — no DNS nameservers configured.\n\n**Root Cause**: resolv.conf has no nameserver entries. This can happen after a failed network reconfiguration or DHCP issue.\n\n**Risk Level**: High\n\n**Evidence**: `dig` timed out. /etc/resolv.conf is empty.\n\n**Remediation**:\n1. Add nameservers: `echo -e 'nameserver 8.8.8.8\\nnameserver 1.1.1.1' | sudo tee /etc/resolv.conf`\n2. Restart networking if using DHCP: `sudo systemctl restart NetworkManager`",
        "dns"
    ))

    c1 = "call_205"
    c2 = "call_206"
    scenarios.append(build_example(
        "Can't resolve hostnames. nslookup fails with timeout.",
        [
            (tc("view", {"file_path": "/etc/resolv.conf"}, c1),
             tr("nameserver 10.255.255.1\nsearch corp.local", c1)),
        ],
        "**Summary**: DNS nameserver 10.255.255.1 is likely unreachable.\n\n**Root Cause**: /etc/resolv.conf points to 10.255.255.1 which may be down or unreachable.\n\n**Risk Level**: High\n\n**Evidence**: /etc/resolv.conf shows nameserver 10.255.255.1.\n\n**Remediation**:\n1. Test: `dig @10.255.255.1 google.com`\n2. If unreachable, add fallback: `echo 'nameserver 8.8.8.8' | sudo tee -a /etc/resolv.conf`",
        "dns"
    ))

    # Teach: "wrong IP" → check /etc/hosts, "can't resolve" → check /etc/resolv.conf
    c1 = "call_207"
    scenarios.append(build_example(
        "DNS resolution fails for all domains. Timeouts everywhere.",
        [
            (tc("view", {"file_path": "/etc/resolv.conf"}, c1),
             tr("nameserver 127.0.0.53", c1)),
        ],
        "**Summary**: DNS resolving through systemd-resolved at 127.0.0.53.\n\n**Root Cause**: The system uses systemd-resolved as a local DNS stub. If it's not running, all DNS fails.\n\n**Evidence**: /etc/resolv.conf points to 127.0.0.53 (systemd-resolved).\n\n**Remediation**:\n1. Check resolved: `systemctl status systemd-resolved`\n2. If down: `sudo systemctl restart systemd-resolved`\n3. Temporary fix: replace with `nameserver 8.8.8.8`",
        "dns"
    ))

    # =========================================================================
    # GAP 3: PERMISSION — ls -la (clean flags) then mount | grep for noexec
    # =========================================================================

    c1 = "call_301"
    c2 = "call_302"
    scenarios.append(build_example(
        "I set permissions on /opt/myapp/run.sh to 755 but it still says permission denied when I run it.",
        [
            (tc("bash", {"command": "ls -la /opt/myapp/run.sh"}, c1),
             tr("-rwxr-xr-x 1 root root 523 Apr 10 10:00 /opt/myapp/run.sh", c1)),
            (tc("bash", {"command": "mount | grep /opt"}, c2),
             tr("/dev/sdb1 on /opt type ext4 (rw,noexec,nosuid,relatime)", c2)),
        ],
        "**Summary**: /opt is mounted with `noexec` flag — scripts cannot execute.\n\n**Root Cause**: File permissions are 755 (correct), but the /opt filesystem is mounted with `noexec`, preventing execution of any binaries or scripts.\n\n**Risk Level**: Medium\n\n**Evidence**: `ls -la` shows 755 permissions. `mount` shows noexec on /opt.\n\n**Remediation**:\n1. Remount: `sudo mount -o remount,exec /opt`\n2. Make permanent: edit /etc/fstab, remove `noexec` from /opt entry\n3. Workaround: `bash /opt/myapp/run.sh`\n\n**Rollback**: `sudo mount -o remount,noexec /opt`",
        "permission"
    ))

    c1 = "call_303"
    c2 = "call_304"
    scenarios.append(build_example(
        "Script has 755 permissions but still permission denied. What else could cause this?",
        [
            (tc("bash", {"command": "ls -la /mnt/scripts/deploy.sh"}, c1),
             tr("-rwxr-xr-x 1 deploy deploy 1200 Apr 12 14:30 /mnt/scripts/deploy.sh", c1)),
            (tc("bash", {"command": "mount | grep /mnt"}, c2),
             tr("/dev/sdc1 on /mnt type ext4 (rw,noexec,relatime)", c2)),
        ],
        "**Summary**: /mnt is mounted with noexec — execution blocked at filesystem level.\n\n**Root Cause**: Despite correct file permissions (755), the mount option `noexec` prevents running any executable from /mnt.\n\n**Risk Level**: Medium\n\n**Evidence**: Permissions are 755, mount shows `noexec` on /mnt.\n\n**Remediation**:\n1. `sudo mount -o remount,exec /mnt`\n2. Or copy the script to a non-noexec path: `cp /mnt/scripts/deploy.sh /tmp/ && bash /tmp/deploy.sh`",
        "permission"
    ))

    c1 = "call_305"
    c2 = "call_306"
    scenarios.append(build_example(
        "Permission denied running /data/app/start.sh even though I'm root and permissions are 777.",
        [
            (tc("bash", {"command": "ls -la /data/app/start.sh"}, c1),
             tr("-rwxrwxrwx 1 root root 456 Apr 15 09:00 /data/app/start.sh", c1)),
            (tc("bash", {"command": "mount | grep /data"}, c2),
             tr("/dev/mapper/data-lv on /data type xfs (rw,noexec,nodev,relatime)", c2)),
        ],
        "**Summary**: /data mounted with `noexec` — even root can't execute scripts here.\n\n**Root Cause**: The `noexec` mount flag overrides file permissions.\n\n**Evidence**: 777 permissions, but `mount` shows noexec on /data.\n\n**Remediation**: `sudo mount -o remount,exec /data`",
        "permission"
    ))

    # =========================================================================
    # GAP 4: SERVICE CRASH — fallback when systemctl/journalctl not available
    # Key: try systemctl → fails → grep /var/log/ (correct path with trailing slash or wildcard)
    # =========================================================================

    c1 = "call_401"
    c2 = "call_402"
    scenarios.append(build_example(
        "My application service keeps crashing and restarting. How do I find out why?",
        [
            (tc("bash", {"command": "systemctl status myapp --no-pager"}, c1),
             tr("bash: systemctl: command not found", c1)),
            (tc("grep", {"pattern": "error|fatal|crash|fail", "path": "/var/log/"}, c2),
             tr("/var/log/myapp.log:2026-04-14 10:00:01 FATAL: database connection refused at 10.0.0.5:5432\n/var/log/myapp.log:2026-04-14 10:05:01 FATAL: database connection refused at 10.0.0.5:5432\n/var/log/myapp.log:2026-04-14 10:10:01 FATAL: database connection refused at 10.0.0.5:5432", c2)),
        ],
        "**Summary**: Application crashing every 5 minutes — database connection refused.\n\n**Root Cause**: myapp cannot connect to PostgreSQL at 10.0.0.5:5432. The database may be down or the network path is broken.\n\n**Risk Level**: High — crash loop.\n\n**Evidence**: /var/log/myapp.log shows repeated FATAL errors: \"database connection refused at 10.0.0.5:5432\".\n\n**Remediation**:\n1. Check DB: `nc -zv 10.0.0.5 5432`\n2. Check DB service on the remote host\n3. Verify connection config in myapp settings",
        "service"
    ))

    c1 = "call_403"
    c2 = "call_404"
    scenarios.append(build_example(
        "App crashes on startup. No systemd on this server.",
        [
            (tc("grep", {"pattern": "error|fatal|exception", "path": "/var/log/"}, c1),
             tr("/var/log/app.log:Exception: Cannot bind to 0.0.0.0:3000 — address already in use\n/var/log/app.log:FATAL: Server failed to start", c1)),
        ],
        "**Summary**: App can't start — port 3000 is already in use.\n\n**Root Cause**: Another process is bound to port 3000.\n\n**Evidence**: /var/log/app.log shows \"Cannot bind to 0.0.0.0:3000 — address already in use\".\n\n**Remediation**:\n1. Find the process: `ss -tlnp | grep 3000`\n2. Stop the conflicting process or change the app's port",
        "service"
    ))

    c1 = "call_405"
    c2 = "call_406"
    scenarios.append(build_example(
        "Service keeps dying. How do I check what's wrong?",
        [
            (tc("bash", {"command": "systemctl status myapp --no-pager -n 20"}, c1),
             tr("bash: systemctl: command not found", c1)),
            (tc("glob", {"pattern": "*.log", "path": "/var/log"}, c2),
             tr("/var/log/syslog\n/var/log/auth.log\n/var/log/myapp.log\n/var/log/dpkg.log", c2)),
        ],
        "Check the application log for errors.",
        "service"
    ))

    # Follow-up: after finding logs, grep them
    c1 = "call_407"
    c2 = "call_408"
    c3 = "call_409"
    scenarios.append(build_example(
        "My web app crashes randomly. The logs are in /var/log/webapp/.",
        [
            (tc("grep", {"pattern": "error|fatal|exception|killed|oom", "path": "/var/log/webapp/"}, c1),
             tr("/var/log/webapp/app.log:2026-04-15 03:00:01 ERROR: out of memory allocating 256MB buffer\n/var/log/webapp/app.log:2026-04-15 03:00:01 FATAL: process killed by signal 9", c1)),
        ],
        "**Summary**: App killed by OOM — tried to allocate 256MB buffer.\n\n**Root Cause**: Application ran out of memory allocating a 256MB buffer and was killed by signal 9 (SIGKILL from OOM killer).\n\n**Risk Level**: High\n\n**Evidence**: /var/log/webapp/app.log shows OOM followed by SIGKILL.\n\n**Remediation**:\n1. Check system memory: `free -m`\n2. Increase memory limit or fix the memory-hungry allocation\n3. Add swap as temporary measure",
        "service"
    ))

    # =========================================================================
    # GAP 5: NETWORK — "can't reach internet but local works" → ip route
    # =========================================================================

    c1 = "call_501"
    scenarios.append(build_example(
        "The server can reach other machines on the local network but can't reach the internet.",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("10.0.0.0/24 dev eth0 proto kernel scope link src 10.0.0.5", c1)),
        ],
        "**Summary**: No default gateway — only local network route exists.\n\n**Root Cause**: `ip route` shows no `default via` entry. Without a default gateway, packets to the internet have no route.\n\n**Risk Level**: High\n\n**Evidence**: Only route is 10.0.0.0/24 — no default gateway.\n\n**Remediation**:\n1. Add gateway: `sudo ip route add default via 10.0.0.1 dev eth0`\n2. Make permanent in /etc/netplan/ or /etc/network/interfaces\n3. Test: `ping -c 3 8.8.8.8`\n\n**Rollback**: `sudo ip route del default`",
        "network"
    ))

    c1 = "call_502"
    scenarios.append(build_example(
        "Can't reach external hosts. Local network is fine.",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("192.168.1.0/24 dev ens3 proto kernel scope link src 192.168.1.100", c1)),
        ],
        "**Summary**: Missing default gateway — only local subnet route.\n\n**Root Cause**: No default route to forward internet-bound traffic.\n\n**Evidence**: `ip route` shows only 192.168.1.0/24 local route, no default.\n\n**Remediation**: `sudo ip route add default via 192.168.1.1 dev ens3`",
        "network"
    ))

    c1 = "call_503"
    scenarios.append(build_example(
        "Server can ping 192.168.1.1 but can't ping 8.8.8.8. What's wrong?",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("192.168.1.0/24 dev eth0 proto kernel scope link src 192.168.1.50\n169.254.0.0/16 dev eth0 scope link metric 1000", c1)),
        ],
        "**Summary**: No default gateway configured. Only local and link-local routes exist.\n\n**Root Cause**: Missing `default via` route. Server can reach local subnet but has no path to external networks.\n\n**Evidence**: `ip route` shows no default gateway.\n\n**Remediation**: `sudo ip route add default via 192.168.1.1 dev eth0`",
        "network"
    ))

    c1 = "call_504"
    scenarios.append(build_example(
        "Internet connectivity is down. Can still SSH from the local network.",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("default via 10.0.0.1 dev eth0\n10.0.0.0/24 dev eth0 proto kernel scope link src 10.0.0.5", c1)),
        ],
        "**Summary**: Default gateway exists at 10.0.0.1 — routing looks correct.\n\n**Root Cause**: Routing is configured. The issue may be with the gateway itself, DNS, or an upstream problem.\n\n**Evidence**: `ip route` shows default via 10.0.0.1.\n\n**Remediation**:\n1. Test gateway: `ping -c 3 10.0.0.1`\n2. Test internet: `ping -c 3 8.8.8.8`\n3. If ping works but DNS fails, check /etc/resolv.conf",
        "network"
    ))

    # =========================================================================
    # GAP 6: GIT — use the path mentioned by the user
    # =========================================================================

    c1 = "call_601"
    scenarios.append(build_example(
        "I tried to pull in /home/dev/project but got merge conflicts. Git says I have unmerged paths.",
        [
            (tc("bash", {"command": "cd /home/dev/project && git status"}, c1),
             tr("On branch main\nYou have unmerged paths.\n  (fix conflicts and run \"git commit\")\n\nUnmerged paths:\n  (use \"git add <file>...\" to mark resolution)\n        both modified:   README.md\n        both modified:   src/config.yaml", c1)),
        ],
        "**Summary**: Two merge conflicts in /home/dev/project: README.md and src/config.yaml.\n\n**Root Cause**: Both local and remote branches modified these files.\n\n**Risk Level**: Low — normal git workflow.\n\n**Evidence**: `git status` shows 2 unmerged files.\n\n**Remediation**:\n1. Edit each file and resolve conflict markers (<<<< ==== >>>>)\n2. `git add README.md src/config.yaml`\n3. `git commit -m 'Resolve merge conflicts'`\n\n**Rollback**: `git merge --abort`",
        "git"
    ))

    c1 = "call_602"
    scenarios.append(build_example(
        "Git merge conflict in /srv/app. Can't push my changes.",
        [
            (tc("bash", {"command": "cd /srv/app && git status"}, c1),
             tr("On branch feature-x\nYou have unmerged paths.\n\nUnmerged paths:\n        both modified:   package.json", c1)),
        ],
        "**Summary**: Merge conflict in /srv/app/package.json on branch feature-x.\n\n**Evidence**: `git status` in /srv/app shows package.json is unmerged.\n\n**Remediation**:\n1. Resolve conflicts in package.json\n2. `git add package.json && git commit`",
        "git"
    ))

    c1 = "call_603"
    scenarios.append(build_example(
        "Git pull failed in /var/www/html with merge errors.",
        [
            (tc("bash", {"command": "cd /var/www/html && git status"}, c1),
             tr("On branch main\nYou have unmerged paths.\n\nUnmerged paths:\n        both modified:   index.html\n        both modified:   styles.css\n        both modified:   app.js", c1)),
        ],
        "**Summary**: Three merge conflicts in /var/www/html.\n\n**Evidence**: index.html, styles.css, and app.js all have conflicts.\n\n**Remediation**:\n1. Resolve each file's conflict markers\n2. `git add index.html styles.css app.js`\n3. `git commit -m 'Resolve merge conflicts'`\n\n**Rollback**: `git merge --abort`",
        "git"
    ))

    # Also teach: git status at / fails → try finding the repo
    c1 = "call_604"
    c2 = "call_605"
    scenarios.append(build_example(
        "Git says I have unmerged paths in my project at /opt/myproject.",
        [
            (tc("bash", {"command": "cd /opt/myproject && git status"}, c1),
             tr("On branch develop\nYou have unmerged paths.\n\nUnmerged paths:\n        both modified:   config/settings.py", c1)),
        ],
        "**Summary**: Merge conflict in /opt/myproject/config/settings.py.\n\n**Evidence**: `git status` in /opt/myproject shows settings.py unmerged.\n\n**Remediation**: Resolve the conflict in config/settings.py, then `git add` and `git commit`.",
        "git"
    ))

    # =========================================================================
    # GAP 7: ZOMBIE PROCESSES — correct ps syntax
    # Model was repeating "ps aux --sort=cpu_usage" which is invalid.
    # Correct: ps aux | grep Z, or ps aux | grep defunct
    # =========================================================================

    c1 = "call_701"
    c2 = "call_702"
    scenarios.append(build_example(
        "I see zombie processes in top. How do I find and deal with them?",
        [
            (tc("bash", {"command": "ps aux | grep -E 'Z|defunct'"}, c1),
             tr("root      1234  0.0  0.0      0     0 ?        Z    10:00   0:00 [worker] <defunct>\nroot      1235  0.0  0.0      0     0 ?        Z    10:00   0:00 [worker] <defunct>\nroot      1236  0.0  0.0      0     0 ?        Z    10:00   0:00 [worker] <defunct>\nroot      9999  0.0  0.0  12345   567 pts/0    S+   10:15   0:00 grep -E Z|defunct", c1)),
            (tc("bash", {"command": "ps -o ppid= -p 1234"}, c2),
             tr("   890", c2)),
        ],
        "**Summary**: 3 zombie processes, all children of PID 890.\n\n**Root Cause**: Parent process (PID 890) is not reaping its children. Dead child processes remain in the process table as zombies.\n\n**Risk Level**: Low — zombies use minimal resources but indicate a bug.\n\n**Evidence**: 3 zombie (Z/defunct) processes found. Parent is PID 890.\n\n**Remediation**:\n1. Identify parent: `ps -p 890 -o comm=`\n2. Send SIGCHLD: `sudo kill -SIGCHLD 890`\n3. If that doesn't work, restart the parent: `sudo kill 890`\n\n**Rollback**: N/A",
        "process"
    ))

    c1 = "call_703"
    scenarios.append(build_example(
        "There are zombie processes showing up. What are they and how do I fix it?",
        [
            (tc("bash", {"command": "ps aux | grep defunct"}, c1),
             tr("www-data  5678  0.0  0.0      0     0 ?        Z    08:30   0:00 [php-fpm] <defunct>\nwww-data  5679  0.0  0.0      0     0 ?        Z    08:31   0:00 [php-fpm] <defunct>", c1)),
        ],
        "**Summary**: 2 zombie php-fpm processes found.\n\n**Root Cause**: PHP-FPM worker processes exited but their parent (php-fpm master) hasn't collected their exit status.\n\n**Risk Level**: Low\n\n**Evidence**: `ps aux | grep defunct` shows 2 zombie php-fpm workers.\n\n**Remediation**:\n1. Restart php-fpm: `sudo systemctl restart php-fpm`\n2. This kills the parent and reaps all zombies\n\n**Rollback**: N/A",
        "process"
    ))

    c1 = "call_704"
    scenarios.append(build_example(
        "How do I find zombie processes on this server?",
        [
            (tc("bash", {"command": "ps aux | grep -E 'Z|defunct' | grep -v grep"}, c1),
             tr("", c1)),
        ],
        "**Summary**: No zombie processes found.\n\n**Evidence**: `ps aux | grep defunct` returned no results — system is clean.\n\n**Risk Level**: None.",
        "process"
    ))

    # =========================================================================
    # GAP 8: CPU/PERFORMANCE — uptime then ps aux --sort=-%cpu
    # Model was doing "uptime; free -h" but not following up with process list
    # =========================================================================

    c1 = "call_801"
    c2 = "call_802"
    scenarios.append(build_example(
        "The server is extremely slow. Load average is very high.",
        [
            (tc("bash", {"command": "uptime"}, c1),
             tr(" 10:15:23 up 5 days, 3:42, 2 users, load average: 12.34, 11.87, 10.23", c1)),
            (tc("bash", {"command": "ps aux --sort=-%cpu | head -10"}, c2),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\nnobody    9876 99.9  0.1  12345  1234 ?        R    10:10   5:00 yes > /dev/null\nnobody    9877 99.8  0.1  12345  1234 ?        R    10:10   5:00 yes > /dev/null\nnobody    9878 99.7  0.1  12345  1234 ?        R    10:10   5:00 yes > /dev/null\nroot         1  0.0  0.1 169344 11024 ?        Ss   Apr09   0:10 /sbin/init", c2)),
        ],
        "**Summary**: Three `yes` processes consuming 100% CPU each, causing load of 12+.\n\n**Root Cause**: 3 runaway `yes > /dev/null` processes (PIDs 9876-9878) saturating all CPUs.\n\n**Risk Level**: Critical\n\n**Evidence**: Load average 12.34. `ps` shows three `yes` processes at 99.9% CPU.\n\n**Remediation**:\n1. Kill them: `sudo kill 9876 9877 9878`\n2. Investigate: `ps -p 9876 -o user,ppid,lstart`",
        "performance"
    ))

    c1 = "call_803"
    c2 = "call_804"
    scenarios.append(build_example(
        "Server very sluggish. Load is through the roof.",
        [
            (tc("bash", {"command": "uptime"}, c1),
             tr(" 14:30:00 up 12 days, 1 user, load average: 8.50, 7.90, 6.20", c1)),
            (tc("bash", {"command": "ps aux --sort=-%cpu | head -10"}, c2),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\njenkins   4567 185.0 12.3 8901234 987654 ?   Rl   14:00  55:00 java -jar jenkins.war\nroot         1  0.1  0.0 169344  11024 ?   Ss   Apr03   1:20 /sbin/init", c2)),
        ],
        "**Summary**: Jenkins consuming 185% CPU (multi-threaded), causing load average of 8.5.\n\n**Root Cause**: Java process (jenkins.war) is CPU-bound, likely running a heavy build.\n\n**Risk Level**: High\n\n**Evidence**: `uptime` shows load 8.5. Jenkins at 185% CPU.\n\n**Remediation**:\n1. Check Jenkins UI for runaway builds\n2. If stuck: `sudo kill 4567` to stop Jenkins\n3. Limit concurrent builds in Jenkins config",
        "performance"
    ))

    c1 = "call_805"
    c2 = "call_806"
    scenarios.append(build_example(
        "High CPU usage on production server. Need to find the cause.",
        [
            (tc("bash", {"command": "uptime"}, c1),
             tr(" 09:00:00 up 30 days, load average: 4.20, 3.80, 3.50", c1)),
            (tc("bash", {"command": "ps aux --sort=-%cpu | head -10"}, c2),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\nmysql     2345 120.0 15.2 4567890 1234567 ?  Sl   08:55  6:00 /usr/sbin/mysqld\nwww-data  3456  25.3  5.1 2345678  456789 ?   Sl   08:00 15:00 php-fpm: pool www\nroot         1   0.0  0.0  169344   11024 ?   Ss   Mar17  2:30 /sbin/init", c2)),
        ],
        "**Summary**: MySQL consuming 120% CPU — likely a heavy or runaway query.\n\n**Root Cause**: mysqld is the top CPU consumer at 120%.\n\n**Evidence**: Load average 4.2. MySQL at 120% CPU, PHP-FPM at 25%.\n\n**Remediation**:\n1. Check for slow queries: `mysqladmin processlist`\n2. Kill runaway query if found: `mysqladmin kill <id>`\n3. Review slow query log: `grep -c 'Query_time' /var/log/mysql/slow.log`",
        "performance"
    ))

    # Single-turn: just uptime (low load, no follow-up needed)
    c1 = "call_807"
    scenarios.append(build_example(
        "Is the server under heavy load?",
        [
            (tc("bash", {"command": "uptime"}, c1),
             tr(" 16:00:00 up 7 days, 2 users, load average: 0.45, 0.52, 0.48", c1)),
        ],
        "**Summary**: Load is normal — 0.45 average.\n\n**Evidence**: `uptime` shows load average 0.45, well within normal range.\n\n**Risk Level**: Low — no performance issues.",
        "performance"
    ))

    return scenarios


def main():
    scenarios = generate_scenarios()
    random.seed(43)
    random.shuffle(scenarios)

    n = len(scenarios)
    n_eval = max(2, n // 20)
    n_valid = max(2, n // 20)
    n_train = n - n_eval - n_valid

    train = scenarios[:n_train]
    eval_data = scenarios[n_train:n_train + n_eval]
    valid = scenarios[n_train + n_eval:]

    output_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data_v4")
    os.makedirs(output_dir, exist_ok=True)

    for name, data in [("train_rw2.jsonl", train), ("eval_rw2.jsonl", eval_data), ("valid_rw2.jsonl", valid)]:
        path = os.path.join(output_dir, name)
        with open(path, "w") as f:
            for item in data:
                f.write(json.dumps(item) + "\n")
        print(f"  {name}: {len(data)} examples")

    print(f"\nTotal: {n} examples")
    print(f"  Train: {n_train}, Eval: {n_eval}, Valid: {n_valid}")
    print(f"  Output: {output_dir}/")


if __name__ == "__main__":
    main()
