#!/usr/bin/env python3
"""Termfix Cycle 14v3 — Training data targeting real-world eval gaps.

Generated from analysis of 21 Docker-based real-world scenarios.
Focuses on: correct commands, platform awareness, error recovery, investigation paths.
"""

import json
import os
import random
import sys

# Match the production system prompt from internal/llm/prompt/coder.go
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
    """Build a training example.
    turns = list of (tool_call_msg, tool_response_msg) tuples
    """
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": query},
    ]
    for call_msg, resp_msg in turns:
        messages.append(call_msg)
        messages.append(resp_msg)
    messages.append(asst(final_text))

    return {"messages": messages, "tools": TOOLS, "category": category}


# ============================================================================
# TARGETED TRAINING SCENARIOS — from real-world eval gaps
# ============================================================================

def generate_scenarios():
    scenarios = []

    # =========================================================================
    # 1. DISK: df -i for inode problems (model only knew df -h)
    # =========================================================================
    c1 = "call_1001"
    c2 = "call_1002"
    scenarios.append(build_example(
        "I can't create new files even though df shows plenty of space. 'No space left on device' error.",
        [
            (tc("bash", {"command": "df -i"}, c1),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1      6553600 6553600       0  100% /\ntmpfs          2048000      10 2047990    1% /dev/shm", c1)),
            (tc("bash", {"command": "find / -xdev -maxdepth 3 -type d -exec sh -c 'echo \"$(ls -1 \"$1\" | wc -l) $1\"' _ {} \\; 2>/dev/null | sort -rn | head -10"}, c2),
             tr("50000 /tmp/inode_bomb\n1523 /usr/share/man/man3\n842 /usr/lib/python3\n234 /var/cache/apt/archives", c2)),
        ],
        "**Summary**: Filesystem inodes exhausted — /tmp/inode_bomb contains 50,000 tiny files consuming all available inodes.\n\n**Root Cause**: The /dev/sda1 filesystem has 0 free inodes (100% IUse). The directory /tmp/inode_bomb holds 50,000 files.\n\n**Risk Level**: High — no new files can be created anywhere on /.\n\n**Evidence**: `df -i` shows 0 free inodes on /. `find` located /tmp/inode_bomb with 50,000 entries.\n\n**Remediation**:\n1. `rm -rf /tmp/inode_bomb` to free inodes\n2. Verify with `df -i` that inodes are recovered\n\n**Rollback**: N/A — files in /tmp are temporary.",
        "disk"
    ))

    # Variation: inode problem with different phrasing
    c1 = "call_1003"
    scenarios.append(build_example(
        "Getting 'No space left on device' but disk usage shows only 40%. What's wrong?",
        [
            (tc("bash", {"command": "df -i"}, c1),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1      1310720 1310720       0  100% /\ntmpfs          2048000      10 2047990    1% /dev/shm", c1)),
        ],
        "**Summary**: Inodes exhausted on / despite available disk space.\n\n**Root Cause**: All 1,310,720 inodes on /dev/sda1 are used (100%). This happens when many small files are created. Common causes: session files, temp files, or mail queue buildup.\n\n**Risk Level**: High — no new files can be created.\n\n**Evidence**: `df -i` shows 0 free inodes on /.\n\n**Remediation**:\n1. Find directories with many files: `find / -xdev -type d -exec sh -c 'echo \"$(ls -1 \"$1\" | wc -l) $1\"' _ {} \\; 2>/dev/null | sort -rn | head -10`\n2. Remove unnecessary files from the top directory\n3. Verify: `df -i`\n\n**Rollback**: Only remove files you're certain are unnecessary.",
        "disk"
    ))

    # Variation: user mentions disk full, model should try both df -h and df -i
    c1 = "call_1004"
    c2 = "call_1005"
    scenarios.append(build_example(
        "touch: cannot touch 'test.txt': No space left on device. But I swear the disk isn't full.",
        [
            (tc("bash", {"command": "df -h"}, c1),
             tr("Filesystem      Size  Used Avail Use%  Mounted on\n/dev/sda1       50G   20G   28G  42% /\ntmpfs           4G     0    4G   0% /dev/shm", c1)),
            (tc("bash", {"command": "df -i"}, c2),
             tr("Filesystem      Inodes  IUsed   IFree IUse% Mounted on\n/dev/sda1       3276800 3276800       0  100% /\ntmpfs          1024000      10 1023990    1% /dev/shm", c2)),
        ],
        "**Summary**: Disk space is fine (42% used) but all inodes are exhausted.\n\n**Root Cause**: All 3,276,800 inodes consumed on /dev/sda1. Many small files exist somewhere on the filesystem.\n\n**Risk Level**: High\n\n**Evidence**: `df -h` shows 28G free space, but `df -i` shows 0 free inodes (100% IUse).\n\n**Remediation**:\n1. Find where the small files are: `find / -xdev -type d -size +1M 2>/dev/null | head -20`\n2. Remove the problematic directory once identified\n\n**Rollback**: Only delete files confirmed as unnecessary.",
        "disk"
    ))

    # =========================================================================
    # 2. DNS: resolv.conf vs hosts — model confused which to check
    # =========================================================================
    c1 = "call_2001"
    scenarios.append(build_example(
        "I can't resolve any domain names. ping google.com says 'Temporary failure in name resolution'.",
        [
            (tc("view", {"file_path": "/etc/resolv.conf"}, c1),
             tr("nameserver 192.0.2.1", c1)),
        ],
        "**Summary**: DNS resolution failing due to unreachable nameserver.\n\n**Root Cause**: /etc/resolv.conf points to nameserver 192.0.2.1, which is in the TEST-NET-1 range (RFC 5737) — not a real DNS server.\n\n**Risk Level**: High — no DNS resolution means no internet connectivity by hostname.\n\n**Evidence**: /etc/resolv.conf contains only `nameserver 192.0.2.1`.\n\n**Remediation**:\n1. Edit /etc/resolv.conf: `sudo nano /etc/resolv.conf`\n2. Replace with working nameservers: `nameserver 8.8.8.8` and `nameserver 1.1.1.1`\n3. Test: `dig google.com`\n\n**Rollback**: Restore original nameserver entry.",
        "dns"
    ))

    # DNS: name resolution failure, check resolv.conf first
    c1 = "call_2002"
    scenarios.append(build_example(
        "DNS is broken. Can't reach any websites by name but IP addresses work fine.",
        [
            (tc("view", {"file_path": "/etc/resolv.conf"}, c1),
             tr("# Generated by NetworkManager\nnameserver 10.0.0.1", c1)),
        ],
        "**Summary**: DNS resolution failing — nameserver 10.0.0.1 may be unreachable or misconfigured.\n\n**Root Cause**: /etc/resolv.conf points to 10.0.0.1 as the only nameserver. If this is a local router/gateway, it may have DNS forwarding disabled or be down.\n\n**Risk Level**: High\n\n**Evidence**: /etc/resolv.conf has nameserver 10.0.0.1.\n\n**Remediation**:\n1. Test if nameserver responds: `dig @10.0.0.1 google.com`\n2. If not, add a fallback: `echo 'nameserver 8.8.8.8' | sudo tee -a /etc/resolv.conf`\n3. Or fix the local DNS resolver at 10.0.0.1\n\n**Rollback**: Remove the added nameserver line.",
        "dns"
    ))

    # DNS: wrong IP for a hostname — check /etc/hosts
    c1 = "call_2003"
    scenarios.append(build_example(
        "My app can't connect to api.internal — it resolves to the wrong IP address.",
        [
            (tc("view", {"file_path": "/etc/hosts"}, c1),
             tr("127.0.0.1 localhost\n10.99.99.99 api.internal\n::1 localhost", c1)),
        ],
        "**Summary**: /etc/hosts has a stale entry mapping api.internal to wrong IP.\n\n**Root Cause**: /etc/hosts maps api.internal to 10.99.99.99. If the service has moved to a different IP, this static entry overrides DNS.\n\n**Risk Level**: Medium\n\n**Evidence**: /etc/hosts contains `10.99.99.99 api.internal`.\n\n**Remediation**:\n1. Update /etc/hosts with the correct IP: `sudo nano /etc/hosts`\n2. Or remove the entry to let DNS resolve it naturally\n3. Verify: `getent hosts api.internal`\n\n**Rollback**: Restore the old IP in /etc/hosts.",
        "dns"
    ))

    # DNS: wrong hostname resolution — check hosts first, then resolv.conf
    c1 = "call_2004"
    c2 = "call_2005"
    scenarios.append(build_example(
        "My application is connecting to the wrong server for db.internal. It was migrated to a new IP last week.",
        [
            (tc("view", {"file_path": "/etc/hosts"}, c1),
             tr("127.0.0.1 localhost\n192.168.1.50 db.internal\n::1 localhost ip6-localhost ip6-loopback", c1)),
            (tc("bash", {"command": "getent hosts db.internal"}, c2),
             tr("192.168.1.50    db.internal", c2)),
        ],
        "**Summary**: Stale /etc/hosts entry for db.internal points to old IP 192.168.1.50.\n\n**Root Cause**: /etc/hosts has a static mapping for db.internal → 192.168.1.50. This overrides any DNS changes from the migration.\n\n**Risk Level**: Medium — application is connecting to the wrong database.\n\n**Evidence**: /etc/hosts maps db.internal to 192.168.1.50, confirmed by `getent hosts`.\n\n**Remediation**:\n1. Update the IP in /etc/hosts to the new server address\n2. Or remove the line to use DNS resolution\n3. Restart the application after the change\n\n**Rollback**: Revert /etc/hosts entry to 192.168.1.50.",
        "dns"
    ))

    # =========================================================================
    # 3. NETWORK: ip route for gateway problems
    # =========================================================================
    c1 = "call_3001"
    scenarios.append(build_example(
        "Server can reach local machines but can't reach the internet.",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("10.0.0.0/24 dev eth0 proto kernel scope link src 10.0.0.5", c1)),
        ],
        "**Summary**: No default gateway configured — server can only reach the local 10.0.0.0/24 network.\n\n**Root Cause**: `ip route` shows no default route. Without a gateway, the server can reach machines on 10.0.0.0/24 but cannot route packets to the internet.\n\n**Risk Level**: High — no internet connectivity.\n\n**Evidence**: `ip route` shows only a local network route, no `default via` entry.\n\n**Remediation**:\n1. Add a default gateway: `sudo ip route add default via 10.0.0.1 dev eth0`\n2. Make persistent: add `gateway 10.0.0.1` to /etc/network/interfaces or equivalent\n3. Test: `ping -c 3 8.8.8.8`\n\n**Rollback**: `sudo ip route del default`",
        "network"
    ))

    # Network: no gateway, different phrasing
    c1 = "call_3002"
    c2 = "call_3003"
    scenarios.append(build_example(
        "Can't ping 8.8.8.8 from the server. Local pings work fine.",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("172.16.0.0/16 dev ens3 proto kernel scope link src 172.16.5.10", c1)),
            (tc("bash", {"command": "ip addr show"}, c2),
             tr("2: ens3: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n    inet 172.16.5.10/16 brd 172.16.255.255 scope global ens3", c2)),
        ],
        "**Summary**: No default route — server is isolated to the local 172.16.0.0/16 subnet.\n\n**Root Cause**: Missing default gateway in routing table. Only local subnet route exists.\n\n**Risk Level**: High\n\n**Evidence**: `ip route` shows only 172.16.0.0/16, no default gateway. Interface ens3 is UP.\n\n**Remediation**:\n1. `sudo ip route add default via 172.16.0.1 dev ens3` (adjust gateway IP as needed)\n2. Make persistent in /etc/netplan/ or /etc/network/interfaces\n\n**Rollback**: `sudo ip route del default`",
        "network"
    ))

    # =========================================================================
    # 4. MEMORY: free -m on Linux (NOT vm_stat)
    # =========================================================================
    c1 = "call_4001"
    c2 = "call_4002"
    scenarios.append(build_example(
        "The server is very slow and I think it might be running out of memory.",
        [
            (tc("bash", {"command": "free -m"}, c1),
             tr("              total        used        free      shared  buff/cache   available\nMem:          15884       14920         124         312         839         312\nSwap:          2048        2048           0", c1)),
            (tc("bash", {"command": "ps aux --sort=-%mem | head -10"}, c2),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\nwww-data  1234  5.2 45.3 8234560 7340032 ?   Sl   Apr12  128:45 java -Xmx4g -jar app.jar\nmysql     5678  2.1 25.7 4567890 4170000 ?   Sl   Apr10   89:23 /usr/sbin/mysqld\nroot       789  0.5  8.2 1345678 1330000 ?   Ssl  Apr11   15:30 /usr/bin/prometheus", c2)),
        ],
        "**Summary**: Server critically low on memory — 124 MB free, swap fully exhausted.\n\n**Root Cause**: Java application (PID 1234) using 45.3% of RAM (7GB), MySQL at 25.7% (4GB). Total usage is 14.9GB of 15.8GB with 0 bytes of swap remaining.\n\n**Risk Level**: Critical — OOM killer may start killing processes.\n\n**Evidence**: `free -m` shows 124MB free, 0 swap. `ps` shows java at 7GB, mysql at 4GB.\n\n**Remediation**:\n1. Restart the java application to release memory: `sudo systemctl restart myapp`\n2. Reduce Java heap: change `-Xmx4g` to `-Xmx2g` in service config\n3. Add more swap as temporary relief: `sudo fallocate -l 4G /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile`\n\n**Rollback**: Revert Java heap settings and restart.",
        "memory"
    ))

    # Memory: moderate pressure
    c1 = "call_4003"
    scenarios.append(build_example(
        "System feels sluggish, might be a memory issue.",
        [
            (tc("bash", {"command": "free -m"}, c1),
             tr("              total        used        free      shared  buff/cache   available\nMem:           7891        6234         245         128        1412        1285\nSwap:          2048         890        1158", c1)),
        ],
        "**Summary**: Memory pressure — 245 MB free, 890 MB swap used.\n\n**Root Cause**: System is using 6.2 GB of 7.9 GB RAM with significant swap usage (890 MB). This indicates memory pressure causing disk I/O from swapping.\n\n**Risk Level**: Medium — system is functional but degraded by swap usage.\n\n**Evidence**: `free -m` shows 1285 MB available (including reclaimable buffers), but active swap usage indicates pressure.\n\n**Remediation**:\n1. Identify top memory consumers: `ps aux --sort=-%mem | head -10`\n2. Consider restarting memory-heavy services\n3. Add more RAM if this is a recurring issue\n\n**Rollback**: N/A — read-only investigation.",
        "memory"
    ))

    # =========================================================================
    # 5. SSL: correct openssl syntax
    # =========================================================================
    c1 = "call_5001"
    scenarios.append(build_example(
        "Users are getting SSL certificate errors when connecting to our HTTPS site on port 443.",
        [
            (tc("bash", {"command": "openssl s_client -connect localhost:443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"}, c1),
             tr("notBefore=Jan  1 00:00:00 2024 GMT\nnotAfter=Jan  1 00:00:00 2025 GMT\nsubject=CN = mysite.com", c1)),
        ],
        "**Summary**: SSL certificate expired — valid until Jan 1, 2025.\n\n**Root Cause**: The certificate for mysite.com expired on January 1, 2025. Browsers and clients will reject connections.\n\n**Risk Level**: High — all HTTPS connections fail.\n\n**Evidence**: `openssl x509` shows notAfter=Jan 1 00:00:00 2025 GMT.\n\n**Remediation**:\n1. Renew the certificate via your CA or Let's Encrypt: `sudo certbot renew`\n2. Or generate a new self-signed cert: `sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /etc/ssl/private/server.key -out /etc/ssl/certs/server.crt`\n3. Restart the web server: `sudo systemctl restart nginx`\n\n**Rollback**: Keep old cert files as backup before replacing.",
        "ssl"
    ))

    # SSL: different port
    c1 = "call_5002"
    scenarios.append(build_example(
        "SSL errors on our API server running on port 8443.",
        [
            (tc("bash", {"command": "openssl s_client -connect localhost:8443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"}, c1),
             tr("notBefore=Mar 15 00:00:00 2025 GMT\nnotAfter=Jun 15 00:00:00 2025 GMT\nsubject=CN = api.example.com", c1)),
        ],
        "**Summary**: SSL certificate for api.example.com expiring soon — valid until June 15, 2025.\n\n**Root Cause**: Certificate expires in about 2 months. Some clients may warn about expiring certificates.\n\n**Risk Level**: Medium — not expired yet but needs renewal soon.\n\n**Evidence**: `openssl` shows notAfter=Jun 15 00:00:00 2025 GMT.\n\n**Remediation**:\n1. Schedule certificate renewal before June 15\n2. If using Let's Encrypt: `sudo certbot renew --deploy-hook 'systemctl reload nginx'`\n3. Set up automated renewal: `sudo crontab -e` and add certbot renew\n\n**Rollback**: Keep current cert as backup.",
        "ssl"
    ))

    # SSL: connection refused (no SSL service running)
    c1 = "call_5003"
    c2 = "call_5004"
    scenarios.append(build_example(
        "HTTPS is not working on our server. Clients get connection refused.",
        [
            (tc("bash", {"command": "openssl s_client -connect localhost:443 </dev/null 2>&1 | head -5"}, c1),
             tr("connect: Connection refused\nconnect:errno=111", c1)),
            (tc("bash", {"command": "ss -tlnp | grep -E ':443|:80'"}, c2),
             tr("LISTEN 0      511          0.0.0.0:80        0.0.0.0:*    users:((\"nginx\",pid=1234,fd=6))", c2)),
        ],
        "**Summary**: Nginx is only listening on port 80, not 443 — HTTPS is not configured.\n\n**Root Cause**: No process is listening on port 443. Nginx is running but only on port 80.\n\n**Risk Level**: High — no HTTPS available.\n\n**Evidence**: Connection refused on port 443. `ss` shows nginx listening only on :80.\n\n**Remediation**:\n1. Add SSL configuration to nginx: edit /etc/nginx/sites-enabled/default\n2. Add `listen 443 ssl;` block with certificate paths\n3. `sudo nginx -t && sudo systemctl reload nginx`\n\n**Rollback**: Remove the SSL server block from nginx config.",
        "ssl"
    ))

    # =========================================================================
    # 6. PERMISSION: check permissions AND mount options
    # =========================================================================
    c1 = "call_6001"
    c2 = "call_6002"
    scenarios.append(build_example(
        "I set permissions on /opt/myapp/run.sh to 755 but it still says permission denied when I run it.",
        [
            (tc("bash", {"command": "ls -la /opt/myapp/run.sh"}, c1),
             tr("-rwxr-xr-x 1 root root 523 Apr 10 10:00 /opt/myapp/run.sh", c1)),
            (tc("bash", {"command": "mount | grep /opt"}, c2),
             tr("/dev/sdb1 on /opt type ext4 (rw,noexec,nosuid,relatime)", c2)),
        ],
        "**Summary**: /opt is mounted with `noexec` — scripts cannot be executed from this partition.\n\n**Root Cause**: The filesystem containing /opt/myapp/run.sh is mounted with the `noexec` flag, which prevents executing any binaries or scripts regardless of file permissions.\n\n**Risk Level**: Medium\n\n**Evidence**: File permissions are 755 (correct), but `mount` shows /opt is mounted with `rw,noexec,nosuid`.\n\n**Remediation**:\n1. Remount without noexec: `sudo mount -o remount,exec /opt`\n2. Make permanent: edit /etc/fstab and remove `noexec` from the /opt entry\n3. Or run the script with the interpreter: `bash /opt/myapp/run.sh`\n\n**Rollback**: `sudo mount -o remount,noexec /opt`",
        "permission"
    ))

    # Permission: file ownership
    c1 = "call_6003"
    scenarios.append(build_example(
        "My app can't write to its log file. Permission denied on /var/log/myapp.log.",
        [
            (tc("bash", {"command": "ls -la /var/log/myapp.log"}, c1),
             tr("-rw------- 1 root root 45678 Apr 14 10:00 /var/log/myapp.log", c1)),
        ],
        "**Summary**: Log file owned by root with no group/other permissions — application user cannot write.\n\n**Root Cause**: /var/log/myapp.log has permissions 600 (rw-------) and is owned by root:root. The application likely runs as a different user.\n\n**Risk Level**: Medium — app cannot log, which hinders debugging.\n\n**Evidence**: `ls -la` shows root:root ownership with 600 permissions.\n\n**Remediation**:\n1. Fix ownership: `sudo chown myapp:myapp /var/log/myapp.log`\n2. Or fix permissions: `sudo chmod 666 /var/log/myapp.log`\n3. Better: `sudo chmod 664 /var/log/myapp.log && sudo chown myapp:adm /var/log/myapp.log`\n\n**Rollback**: `sudo chown root:root /var/log/myapp.log && sudo chmod 600 /var/log/myapp.log`",
        "permission"
    ))

    # Permission: no permissions at all
    c1 = "call_6004"
    c2 = "call_6005"
    scenarios.append(build_example(
        "Permission denied when trying to read /var/log/myapp.log. I need to check the error logs.",
        [
            (tc("bash", {"command": "ls -la /var/log/myapp.log"}, c1),
             tr("---------- 1 root root 12345 Apr 14 08:00 /var/log/myapp.log", c1)),
            (tc("bash", {"command": "id"}, c2),
             tr("uid=1000(dev) gid=1000(dev) groups=1000(dev),27(sudo)", c2)),
        ],
        "**Summary**: Log file has no permissions (mode 000) — nobody can read it.\n\n**Root Cause**: /var/log/myapp.log has permissions ---------- (000). This was likely set accidentally with `chmod 000`.\n\n**Risk Level**: Medium\n\n**Evidence**: `ls -la` shows mode 000. Current user is dev (uid=1000) with sudo access.\n\n**Remediation**:\n1. Restore permissions: `sudo chmod 644 /var/log/myapp.log`\n2. Then read: `cat /var/log/myapp.log`\n\n**Rollback**: `sudo chmod 000 /var/log/myapp.log` (not recommended).",
        "permission"
    ))

    # =========================================================================
    # 7. SERVICE: fallback when journalctl not available
    # =========================================================================
    c1 = "call_7001"
    c2 = "call_7002"
    scenarios.append(build_example(
        "My application service keeps crashing and restarting. How do I find out why?",
        [
            (tc("bash", {"command": "systemctl status myapp --no-pager -n 20"}, c1),
             tr("● myapp.service - My Application\n   Loaded: loaded (/etc/systemd/system/myapp.service; enabled)\n   Active: activating (auto-restart) (Result: exit-code)\n   Process: 12345 ExecStart=/usr/local/bin/myapp.sh (code=exited, status=1/FAILURE)\n   Main PID: 12345 (code=exited, status=1/FAILURE)\n\nApr 14 10:00:01 server myapp.sh[12345]: FATAL: database connection refused at 10.0.0.5:5432\nApr 14 10:00:01 server systemd[1]: myapp.service: Main process exited, code=exited, status=1/FAILURE\nApr 14 10:00:01 server systemd[1]: myapp.service: Failed with result 'exit-code'.", c1)),
        ],
        "**Summary**: Application crashing because database connection to 10.0.0.5:5432 is refused.\n\n**Root Cause**: myapp.service exits with FATAL error — can't connect to PostgreSQL at 10.0.0.5:5432. The database server may be down or the IP/port is wrong.\n\n**Risk Level**: High — service is in a crash loop.\n\n**Evidence**: systemctl status shows exit code 1 with error: \"database connection refused at 10.0.0.5:5432\".\n\n**Remediation**:\n1. Check if the database is running: `systemctl status postgresql` on the DB server\n2. Test connectivity: `nc -zv 10.0.0.5 5432`\n3. Check firewall: `sudo iptables -L -n | grep 5432`\n4. Verify connection string in the app config\n\n**Rollback**: N/A — diagnostic only.",
        "service"
    ))

    # Service: journalctl not available, check /var/log
    c1 = "call_7003"
    c2 = "call_7004"
    c3 = "call_7005"
    scenarios.append(build_example(
        "My app keeps crashing. I can't figure out why.",
        [
            (tc("bash", {"command": "systemctl status myapp --no-pager -n 20"}, c1),
             tr("bash: systemctl: command not found", c1)),
            (tc("grep", {"pattern": "error|fatal|crash|fail", "path": "/var/log/"}, c2),
             tr("/var/log/myapp.log:Apr 14 10:00:01 FATAL: out of memory\n/var/log/myapp.log:Apr 14 10:05:01 FATAL: out of memory\n/var/log/myapp.log:Apr 14 10:10:01 FATAL: out of memory", c2)),
        ],
        "**Summary**: Application crashing repeatedly due to out-of-memory errors.\n\n**Root Cause**: /var/log/myapp.log shows repeated \"FATAL: out of memory\" errors every 5 minutes. The application is running out of memory and crashing.\n\n**Risk Level**: High — continuous crash loop.\n\n**Evidence**: `grep` in /var/log/ found \"FATAL: out of memory\" at 10:00, 10:05, 10:10.\n\n**Remediation**:\n1. Check memory: `free -m`\n2. Increase application memory limits in its config\n3. Check for memory leaks in the application\n4. Consider adding swap as temporary relief\n\n**Rollback**: N/A — diagnostic only.",
        "service"
    ))

    # Service: nginx config error
    c1 = "call_7006"
    c2 = "call_7007"
    scenarios.append(build_example(
        "Nginx won't start. I get an error when running systemctl start nginx.",
        [
            (tc("bash", {"command": "nginx -t"}, c1),
             tr("nginx: [emerg] unknown directive \"server_nme\" in /etc/nginx/sites-enabled/default:3\nnginx: configuration file /etc/nginx/nginx.conf test failed", c1)),
        ],
        "**Summary**: Nginx config has a typo — `server_nme` should be `server_name`.\n\n**Root Cause**: Typo in /etc/nginx/sites-enabled/default line 3: `server_nme` instead of `server_name`.\n\n**Risk Level**: Medium — nginx is completely down.\n\n**Evidence**: `nginx -t` reports unknown directive \"server_nme\".\n\n**Remediation**:\n1. Fix the typo: `sudo sed -i 's/server_nme/server_name/' /etc/nginx/sites-enabled/default`\n2. Test: `sudo nginx -t`\n3. Start: `sudo systemctl start nginx`\n\n**Rollback**: Revert the config change.",
        "service"
    ))

    # =========================================================================
    # 8. GIT: use paths from user input
    # =========================================================================
    c1 = "call_8001"
    scenarios.append(build_example(
        "I tried to pull in /home/dev/project but got merge conflicts. Git says I have unmerged paths.",
        [
            (tc("bash", {"command": "cd /home/dev/project && git status"}, c1),
             tr("On branch main\nYou have unmerged paths.\n  (fix conflicts and run \"git commit\")\n\nUnmerged paths:\n  (use \"git add <file>...\" to mark resolution)\n        both modified:   README.md\n        both modified:   config.yaml", c1)),
        ],
        "**Summary**: Two merge conflicts in /home/dev/project: README.md and config.yaml.\n\n**Root Cause**: A `git pull` or `git merge` resulted in conflicts in README.md and config.yaml — both local and remote branches modified these files.\n\n**Risk Level**: Low — normal git workflow, not a system failure.\n\n**Evidence**: `git status` shows 2 unmerged files: README.md and config.yaml.\n\n**Remediation**:\n1. Open each file and resolve the conflict markers (<<<< ==== >>>>)\n2. `cd /home/dev/project`\n3. `git add README.md config.yaml`\n4. `git commit -m 'Resolve merge conflicts'`\n\n**Rollback**: `git merge --abort` to undo the merge entirely.",
        "git"
    ))

    # Git: detached head
    c1 = "call_8002"
    scenarios.append(build_example(
        "Git says I'm in a detached HEAD state. How do I fix this?",
        [
            (tc("bash", {"command": "git status"}, c1),
             tr("HEAD detached at a1b2c3d\nnothing to commit, working tree clean", c1)),
        ],
        "**Summary**: Git is in detached HEAD state at commit a1b2c3d.\n\n**Root Cause**: You checked out a specific commit or tag instead of a branch. Any new commits won't belong to any branch.\n\n**Risk Level**: Low — no data loss, but new commits could be orphaned.\n\n**Evidence**: `git status` shows HEAD detached at a1b2c3d.\n\n**Remediation**:\n1. Create a branch from current position: `git checkout -b my-branch`\n2. Or return to main: `git checkout main`\n3. If you made commits in detached state, create a branch first to save them\n\n**Rollback**: N/A — git reflog preserves all commits.",
        "git"
    ))

    # =========================================================================
    # 9. CRON: check executable permissions on scripts
    # =========================================================================
    c1 = "call_9001"
    c2 = "call_9002"
    scenarios.append(build_example(
        "My backup cron job hasn't run in 3 days. It should run every night at 2am.",
        [
            (tc("bash", {"command": "crontab -l"}, c1),
             tr("0 2 * * * /usr/local/bin/backup.sh", c1)),
            (tc("bash", {"command": "ls -la /usr/local/bin/backup.sh"}, c2),
             tr("-rw-r--r-- 1 root root 234 Apr 10 15:00 /usr/local/bin/backup.sh", c2)),
        ],
        "**Summary**: Backup script exists in crontab but is not executable.\n\n**Root Cause**: /usr/local/bin/backup.sh has permissions 644 (rw-r--r--) — missing the execute bit. Cron cannot run it.\n\n**Risk Level**: Medium — backups have not run for 3 days.\n\n**Evidence**: `crontab -l` shows the job is scheduled at 2 AM. `ls -la` shows the script is not executable (644).\n\n**Remediation**:\n1. Make executable: `sudo chmod +x /usr/local/bin/backup.sh`\n2. Test manually: `sudo /usr/local/bin/backup.sh`\n3. Check cron log: `grep CRON /var/log/syslog | tail -20`\n\n**Rollback**: `sudo chmod -x /usr/local/bin/backup.sh`",
        "cron"
    ))

    # Cron: wrong cron syntax
    c1 = "call_9003"
    scenarios.append(build_example(
        "My scheduled task isn't running. It should execute every 5 minutes.",
        [
            (tc("bash", {"command": "crontab -l"}, c1),
             tr("5 * * * * /opt/scripts/healthcheck.sh\n# Run every 5 minutes", c1)),
        ],
        "**Summary**: Cron syntax error — runs at minute 5 of every hour, not every 5 minutes.\n\n**Root Cause**: `5 * * * *` means \"at minute 5 of every hour\" (e.g., 1:05, 2:05). For every 5 minutes, it should be `*/5 * * * *`.\n\n**Risk Level**: Low — task runs 12x/day instead of 288x/day.\n\n**Evidence**: `crontab -l` shows `5 * * * *` instead of `*/5 * * * *`.\n\n**Remediation**:\n1. Edit crontab: `crontab -e`\n2. Change `5 * * * *` to `*/5 * * * *`\n\n**Rollback**: Change back to `5 * * * *`.",
        "cron"
    ))

    # =========================================================================
    # 10. PROCESS: zombies
    # =========================================================================
    c1 = "call_10001"
    c2 = "call_10002"
    scenarios.append(build_example(
        "I see zombie processes in top. How do I find and deal with them?",
        [
            (tc("bash", {"command": "ps aux | grep -E 'Z|defunct' | head -20"}, c1),
             tr("root      1234  0.0  0.0      0     0 ?        Z    10:00   0:00 [worker] <defunct>\nroot      1235  0.0  0.0      0     0 ?        Z    10:00   0:00 [worker] <defunct>\nroot      1236  0.0  0.0      0     0 ?        Z    10:00   0:00 [worker] <defunct>", c1)),
            (tc("bash", {"command": "ps -o ppid= -p 1234"}, c2),
             tr("   890", c2)),
        ],
        "**Summary**: 3 zombie processes found, all children of PID 890.\n\n**Root Cause**: Parent process (PID 890) is not collecting exit status of its child processes. The children have exited but remain in the process table as zombies.\n\n**Risk Level**: Low — zombies consume minimal resources (just a PID table entry), but indicate a buggy parent.\n\n**Evidence**: `ps aux` shows 3 zombie (Z) processes. `ps -o ppid` shows parent is PID 890.\n\n**Remediation**:\n1. Identify parent: `ps -p 890 -o comm=`\n2. Restart the parent to clear zombies: `sudo kill -HUP 890`\n3. If that doesn't work: `sudo kill 890` (zombies will be reaped by init)\n4. Fix the application to properly wait() for children\n\n**Rollback**: N/A — restarting the parent is the fix.",
        "process"
    ))

    # =========================================================================
    # 11. PACKAGE: dpkg interrupted
    # =========================================================================
    c1 = "call_11001"
    scenarios.append(build_example(
        "I can't install any packages. apt says dpkg was interrupted.",
        [
            (tc("bash", {"command": "dpkg --configure -a 2>&1 | head -20"}, c1),
             tr("Setting up libc6 (2.35-0ubuntu3) ...\ndpkg: error processing package libc6 (--configure):\n subprocess installed post-installation script returned error exit status 1\nErrors were encountered while processing:\n libc6", c1)),
        ],
        "**Summary**: dpkg is in a broken state — libc6 post-installation script failed.\n\n**Root Cause**: A previous apt/dpkg operation was interrupted, leaving libc6 in a half-configured state. The post-installation script is failing.\n\n**Risk Level**: High — no packages can be installed or updated.\n\n**Evidence**: `dpkg --configure -a` shows libc6 post-install script failed.\n\n**Remediation**:\n1. Try force configure: `sudo dpkg --configure -a --force-confold`\n2. Fix broken packages: `sudo apt-get -f install`\n3. If still stuck: `sudo apt-get clean && sudo apt-get update`\n4. Last resort: `sudo dpkg --remove --force-remove-reinstreq libc6` then reinstall\n\n**Rollback**: Packages cannot easily be rolled back; ensure backups exist.",
        "package"
    ))

    # =========================================================================
    # 12. DOCKER: check logs for OOM/exit codes
    # =========================================================================
    c1 = "call_12001"
    scenarios.append(build_example(
        "My Docker container keeps getting killed. I think it's running out of memory.",
        [
            (tc("bash", {"command": "docker inspect --format '{{.State.Status}} exit:{{.State.ExitCode}} oom:{{.State.OOMKilled}}' myapp"}, c1),
             tr("exited exit:137 oom:true", c1)),
        ],
        "**Summary**: Docker container 'myapp' was OOM-killed (exit code 137).\n\n**Root Cause**: Container ran out of its memory limit and was killed by the OOM killer. Exit code 137 = SIGKILL (128+9).\n\n**Risk Level**: High — container cannot stay running.\n\n**Evidence**: `docker inspect` shows OOMKilled=true, exit code 137.\n\n**Remediation**:\n1. Increase memory limit: `docker run --memory=2g myapp` or update docker-compose.yml\n2. Check for memory leaks in the application\n3. Monitor: `docker stats myapp`\n\n**Rollback**: Revert memory limit to original value.",
        "docker"
    ))

    # Docker: container not starting
    c1 = "call_12002"
    c2 = "call_12003"
    scenarios.append(build_example(
        "Docker container exits immediately after starting.",
        [
            (tc("bash", {"command": "docker ps -a --format 'table {{.Names}}\\t{{.Status}}\\t{{.Image}}' | head -10"}, c1),
             tr("NAMES     STATUS                     IMAGE\nmyapp     Exited (1) 2 minutes ago   myapp:latest\ndb        Up 3 hours                 postgres:14", c1)),
            (tc("bash", {"command": "docker logs myapp --tail 20"}, c2),
             tr("Error: ENOENT: no such file or directory, open '/app/config.json'\n    at Object.openSync (node:fs:600:3)\n    at readFileSync (node:fs:468:35)", c2)),
        ],
        "**Summary**: Container exits because /app/config.json is missing.\n\n**Root Cause**: Application requires /app/config.json but the file doesn't exist in the container image.\n\n**Risk Level**: Medium — container cannot start.\n\n**Evidence**: `docker logs` shows ENOENT error for /app/config.json.\n\n**Remediation**:\n1. Mount a config file: `docker run -v ./config.json:/app/config.json myapp`\n2. Or add it to the Dockerfile: `COPY config.json /app/config.json`\n3. Rebuild: `docker build -t myapp .`\n\n**Rollback**: Use the previous working image version.",
        "docker"
    ))

    # =========================================================================
    # 13. PERFORMANCE: top/ps for CPU hogs
    # =========================================================================
    c1 = "call_13001"
    c2 = "call_13002"
    scenarios.append(build_example(
        "The server is extremely slow. Load average is very high.",
        [
            (tc("bash", {"command": "uptime"}, c1),
             tr(" 10:15:23 up 5 days, 3:42, 2 users, load average: 12.34, 11.87, 10.23", c1)),
            (tc("bash", {"command": "ps aux --sort=-%cpu | head -10"}, c2),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\nnobody    9876 99.9  0.1  12345  1234 ?        R    10:10   5:00 yes > /dev/null\nnobody    9877 99.8  0.1  12345  1234 ?        R    10:10   5:00 yes > /dev/null\nnobody    9878 99.7  0.1  12345  1234 ?        R    10:10   5:00 yes > /dev/null\nroot         1  0.0  0.1 169344 11024 ?        Ss   Apr09   0:10 /sbin/init", c2)),
        ],
        "**Summary**: Three `yes` processes consuming 100% CPU each, causing load average of 12+.\n\n**Root Cause**: Three `yes > /dev/null` processes (PIDs 9876-9878) are consuming all CPU. These are either malicious or a runaway script.\n\n**Risk Level**: Critical — server is nearly unresponsive.\n\n**Evidence**: Load average 12.34 (extremely high). `ps` shows three `yes` processes at 99.9% CPU each.\n\n**Remediation**:\n1. Kill the processes: `sudo kill 9876 9877 9878`\n2. Investigate who started them: `ps -p 9876 -o user,ppid,lstart`\n3. Check for unauthorized access if unexpected\n\n**Rollback**: N/A — killing runaway processes is safe.",
        "performance"
    ))

    # =========================================================================
    # 14. BUILD: check package.json in the right directory
    # =========================================================================
    c1 = "call_14001"
    scenarios.append(build_example(
        "My npm build is failing in /home/dev/myapp. It says 'Module not found: Can't resolve react-dom'.",
        [
            (tc("view", {"file_path": "/home/dev/myapp/package.json"}, c1),
             tr("{\"name\": \"myapp\", \"dependencies\": {\"react\": \"^18.0.0\"}}", c1)),
        ],
        "**Summary**: react-dom is missing from package.json dependencies.\n\n**Root Cause**: package.json lists `react` but not `react-dom`. React applications need both packages.\n\n**Risk Level**: Low — easy fix.\n\n**Evidence**: /home/dev/myapp/package.json shows only `react` in dependencies, no `react-dom`.\n\n**Remediation**:\n1. `cd /home/dev/myapp`\n2. `npm install react-dom`\n3. This adds react-dom to package.json and installs it\n4. Retry the build: `npm run build`\n\n**Rollback**: `npm uninstall react-dom`",
        "build"
    ))

    # Build: Python missing dependency
    c1 = "call_14002"
    scenarios.append(build_example(
        "My Python app crashes on startup with ModuleNotFoundError: No module named 'redis'.",
        [
            (tc("bash", {"command": "pip list 2>/dev/null | grep -i redis"}, c1),
             tr("", c1)),
        ],
        "**Summary**: Python redis package not installed.\n\n**Root Cause**: The application imports `redis` but it's not installed in the current Python environment.\n\n**Risk Level**: Low — easy fix.\n\n**Evidence**: `pip list | grep redis` returned empty — package not installed.\n\n**Remediation**:\n1. Install: `pip install redis`\n2. Or if using requirements.txt: check it includes `redis` and run `pip install -r requirements.txt`\n3. If using a virtualenv, ensure it's activated first\n\n**Rollback**: `pip uninstall redis`",
        "build"
    ))

    # =========================================================================
    # 15. KNOWLEDGE: better OOM killer explanation
    # =========================================================================
    scenarios.append(build_example(
        "What is the Linux OOM killer and how does it decide which process to kill?",
        [],  # No tool calls — knowledge question
        "The Linux OOM (Out-Of-Memory) killer is a kernel mechanism that terminates processes when the system runs critically low on memory.\n\n**How it works:**\n1. When the kernel can't allocate memory and has no swap left, it invokes the OOM killer\n2. Each process gets an `oom_score` (0-1000) based on:\n   - Memory usage (RSS) — larger processes score higher\n   - Process age — newer processes score higher\n   - User adjustments via `oom_score_adj` (-1000 to 1000)\n3. The process with the highest `oom_score` gets killed with SIGKILL (signal 9, exit code 137)\n\n**Key files:**\n- `/proc/<pid>/oom_score` — current OOM score\n- `/proc/<pid>/oom_score_adj` — user adjustment (-1000 = never kill, 1000 = always prefer)\n\n**Protection:** Set `oom_score_adj` to -1000 for critical services:\n```\necho -1000 > /proc/<pid>/oom_score_adj\n```\n\nOr in systemd: `OOMScoreAdjust=-1000` in the service file.\n\n**Detection:** Check `dmesg | grep -i oom` or `journalctl -k | grep -i oom`.",
        "knowledge"
    ))

    # Knowledge: TCP states
    scenarios.append(build_example(
        "Explain the difference between TIME_WAIT and CLOSE_WAIT in TCP connections.",
        [],
        "**TIME_WAIT**: The side that *initiates* the close (sends first FIN) enters TIME_WAIT after receiving the final ACK. It waits for 2×MSL (typically 60 seconds) to ensure all packets from the connection have expired on the network. This is normal and expected — a server with many TIME_WAIT connections is simply handling many short-lived connections.\n\n**CLOSE_WAIT**: The side that *receives* the FIN but hasn't sent its own FIN yet. This means the remote end closed the connection, but the local application hasn't called `close()` on the socket. CLOSE_WAIT connections that persist indicate a **bug** — the application is not properly closing sockets.\n\n**Key difference:**\n- TIME_WAIT = normal, self-resolving (kernel timer)\n- CLOSE_WAIT = application bug, requires code fix\n\n**Diagnosis:**\n```\nss -tan state time-wait | wc -l    # count TIME_WAIT\nss -tan state close-wait | wc -l   # count CLOSE_WAIT\n```\n\nMany CLOSE_WAIT? Find the culprit: `ss -tan state close-wait -p` to see which process holds them.",
        "knowledge"
    ))

    # Knowledge: systemd
    scenarios.append(build_example(
        "How do systemd services work? What's the difference between enable and start?",
        [],
        "**systemd** manages services (daemons) on modern Linux systems.\n\n**Key commands:**\n- `systemctl start nginx` — Start the service NOW\n- `systemctl stop nginx` — Stop the service NOW\n- `systemctl enable nginx` — Start the service automatically on boot\n- `systemctl disable nginx` — Don't start on boot\n- `systemctl restart nginx` — Stop then start\n- `systemctl reload nginx` — Reload config without stopping\n- `systemctl status nginx` — Show current state and recent logs\n\n**start vs enable:**\n- `start` = immediate, temporary (until reboot)\n- `enable` = persistent, takes effect on next boot\n- You usually want both: `systemctl enable --now nginx`\n\n**Service files** live in:\n- `/etc/systemd/system/` — admin-created (highest priority)\n- `/lib/systemd/system/` — package-installed\n\n**Debugging:**\n- `systemctl --failed` — list all failed services\n- `journalctl -u nginx -n 50` — last 50 log lines for nginx\n- `systemctl cat nginx` — show the service file",
        "knowledge"
    ))

    # Knowledge: file descriptors / ulimits
    scenarios.append(build_example(
        "What are file descriptors and why does 'too many open files' happen?",
        [],
        "**File descriptors** (FDs) are integer handles the kernel assigns to open files, sockets, pipes, and other I/O resources. Each process has a limit on how many it can have open.\n\n**Why 'too many open files' happens:**\n- Default per-process limit is often 1024 FDs (`ulimit -n`)\n- Applications that open many connections (databases, web servers) can hit this\n- File descriptor leaks (opening without closing) cause gradual exhaustion\n\n**Check current limits:**\n```\nulimit -n                          # soft limit for current shell\nulimit -Hn                         # hard limit\ncat /proc/<pid>/limits             # limits for a specific process\nls -la /proc/<pid>/fd | wc -l      # actual FDs open by a process\n```\n\n**Fix:**\n1. Temporary: `ulimit -n 65536` (current shell only)\n2. Permanent: edit `/etc/security/limits.conf`:\n   ```\n   * soft nofile 65536\n   * hard nofile 65536\n   ```\n3. For systemd services: add `LimitNOFILE=65536` to the service file\n\n**Find leakers:** `lsof -p <pid> | wc -l` or `ls /proc/<pid>/fd | wc -l`",
        "knowledge"
    ))

    # =========================================================================
    # 16. PLATFORM AWARENESS: Linux vs macOS
    # =========================================================================
    # These teach the model that vm_stat is macOS, free -m is Linux
    c1 = "call_16001"
    scenarios.append(build_example(
        "How much memory is being used on this Linux server?",
        [
            (tc("bash", {"command": "free -m"}, c1),
             tr("              total        used        free      shared  buff/cache   available\nMem:           3944        1234         567         128        2143        2400\nSwap:          2048           0        2048", c1)),
        ],
        "**Summary**: Memory usage is healthy — 1.2 GB used of 3.9 GB, with 2.4 GB available.\n\n**Root Cause**: No memory pressure. 2.1 GB is in buffer/cache (reclaimable if needed). No swap usage.\n\n**Risk Level**: Low — system has plenty of available memory.\n\n**Evidence**: `free -m` shows 2400 MB available, 0 swap used.",
        "memory"
    ))

    # Platform: macOS memory check
    c1 = "call_16002"
    scenarios.append(build_example(
        "Check memory usage on my Mac.",
        [
            (tc("bash", {"command": "vm_stat"}, c1),
             tr("Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:                               12345.\nPages active:                            234567.\nPages inactive:                           45678.\nPages speculative:                         1234.\nPages wired down:                         98765.\nPages occupied by compressor:             23456.", c1)),
        ],
        "**Summary**: macOS memory usage — calculate from vm_stat page counts × 16384 bytes per page.\n\n**Root Cause**: N/A — this is a status check.\n\n**Evidence**: vm_stat shows pages active=234567, wired=98765, free=12345 (page size 16384).\n- Active: ~3.6 GB\n- Wired: ~1.5 GB\n- Free: ~190 MB\n\n**Remediation**: If memory is low, close unused applications or check `ps aux --sort=-%mem | head -10` for top consumers.",
        "memory"
    ))

    # =========================================================================
    # 17. SINGLE-COMMAND DISCIPLINE: teach model to use ONE clean command
    # =========================================================================
    # These examples reinforce that the model should output ONE simple command

    c1 = "call_17001"
    scenarios.append(build_example(
        "Check disk space on the server.",
        [
            (tc("bash", {"command": "df -h"}, c1),
             tr("Filesystem      Size  Used Avail Use%  Mounted on\n/dev/sda1       100G   45G   50G  48% /\n/dev/sdb1       500G  200G  280G  42% /data\ntmpfs            8G     0    8G   0% /dev/shm", c1)),
        ],
        "**Summary**: Disk usage is healthy — root at 48%, /data at 42%.\n\n**Evidence**: `df -h` shows no partitions above 80%.\n\n**Risk Level**: Low.",
        "disk"
    ))

    c1 = "call_17002"
    scenarios.append(build_example(
        "Show me what processes are using the most CPU.",
        [
            (tc("bash", {"command": "ps aux --sort=-%cpu | head -10"}, c1),
             tr("USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND\nwww-data  1234 15.2  8.3 456789 123456 ?   Sl   10:00  45:23 /usr/bin/python3 app.py\nmysql     5678  5.1  4.2 345678  65432 ?   Sl   09:00  30:12 /usr/sbin/mysqld\nroot         1  0.0  0.1 169344  11024 ?   Ss   Apr10   0:10 /sbin/init", c1)),
        ],
        "**Summary**: Python app at 15.2% CPU, MySQL at 5.1% — both within normal range.\n\n**Evidence**: `ps aux --sort=-%cpu` shows top consumer is app.py at 15.2%.\n\n**Risk Level**: Low — no runaway processes.",
        "performance"
    ))

    c1 = "call_17003"
    scenarios.append(build_example(
        "What's listening on port 3000?",
        [
            (tc("bash", {"command": "ss -tlnp | grep 3000"}, c1),
             tr("LISTEN 0      511          0.0.0.0:3000      0.0.0.0:*    users:((\"node\",pid=2345,fd=19))", c1)),
        ],
        "**Summary**: Node.js process (PID 2345) is listening on port 3000.\n\n**Evidence**: `ss -tlnp` shows node process on 0.0.0.0:3000.",
        "port"
    ))

    c1 = "call_17004"
    scenarios.append(build_example(
        "Check the routing table.",
        [
            (tc("bash", {"command": "ip route"}, c1),
             tr("default via 10.0.0.1 dev eth0\n10.0.0.0/24 dev eth0 proto kernel scope link src 10.0.0.5\n172.17.0.0/16 dev docker0 proto kernel scope link src 172.17.0.1", c1)),
        ],
        "**Summary**: Routing looks normal — default gateway is 10.0.0.1 via eth0, Docker network at 172.17.0.0/16.\n\n**Evidence**: `ip route` shows default gateway and two local networks.",
        "network"
    ))

    c1 = "call_17005"
    scenarios.append(build_example(
        "Show me the DNS configuration.",
        [
            (tc("view", {"file_path": "/etc/resolv.conf"}, c1),
             tr("# Generated by NetworkManager\nnameserver 8.8.8.8\nnameserver 8.8.4.4\nsearch example.com", c1)),
        ],
        "**Summary**: DNS configured with Google DNS servers (8.8.8.8 and 8.8.4.4).\n\n**Evidence**: /etc/resolv.conf shows two nameservers and search domain example.com.",
        "dns"
    ))

    # =========================================================================
    # 18. ERROR RECOVERY: when first command fails, try alternatives
    # =========================================================================
    c1 = "call_18001"
    c2 = "call_18002"
    scenarios.append(build_example(
        "Why is my service failing?",
        [
            (tc("bash", {"command": "systemctl status myapp --no-pager -n 20"}, c1),
             tr("bash: systemctl: command not found", c1)),
            (tc("grep", {"pattern": "error|fail|fatal", "path": "/var/log"}, c2),
             tr("/var/log/myapp.log:2026-04-14 10:00:01 ERROR: Failed to bind to port 8080: Address already in use\n/var/log/myapp.log:2026-04-14 10:00:01 FATAL: Server startup failed", c2)),
        ],
        "**Summary**: Service fails to start — port 8080 is already in use.\n\n**Root Cause**: Another process is already bound to port 8080, preventing the application from starting.\n\n**Risk Level**: Medium\n\n**Evidence**: /var/log/myapp.log shows \"Failed to bind to port 8080: Address already in use\".\n\n**Remediation**:\n1. Find what's using port 8080: `ss -tlnp | grep 8080`\n2. Stop the conflicting process or change the application's port\n\n**Rollback**: N/A — diagnostic only.",
        "service"
    ))

    # Error recovery: dig fails, try nslookup
    c1 = "call_18003"
    c2 = "call_18004"
    scenarios.append(build_example(
        "DNS lookup for our domain is failing.",
        [
            (tc("bash", {"command": "dig example.com"}, c1),
             tr(";; connection timed out; no servers could be reached", c1)),
            (tc("view", {"file_path": "/etc/resolv.conf"}, c2),
             tr("nameserver 10.255.255.1", c2)),
        ],
        "**Summary**: DNS resolution failing — nameserver 10.255.255.1 is unreachable.\n\n**Root Cause**: /etc/resolv.conf points to nameserver 10.255.255.1 which is not responding. This caused `dig` to time out.\n\n**Risk Level**: High — no DNS resolution.\n\n**Evidence**: `dig` timed out. /etc/resolv.conf shows only one nameserver at 10.255.255.1.\n\n**Remediation**:\n1. Add a working nameserver: `echo 'nameserver 8.8.8.8' | sudo tee -a /etc/resolv.conf`\n2. Or replace the bad nameserver\n3. Test: `dig example.com @8.8.8.8`\n\n**Rollback**: Remove the added nameserver.",
        "dns"
    ))

    # =========================================================================
    # 19. NEGATIVE EXAMPLES: don't chain commands, use single clean probes
    # =========================================================================
    # These are important to prevent the model from generating
    # "df -h && du -h / && ls -la /var/log" style chains

    c1 = "call_19001"
    scenarios.append(build_example(
        "Is the disk full?",
        [
            (tc("bash", {"command": "df -h"}, c1),
             tr("Filesystem      Size  Used Avail Use%  Mounted on\n/dev/sda1       100G   94G   6G   94% /", c1)),
        ],
        "**Summary**: Root partition at 94% — only 6 GB remaining.\n\n**Risk Level**: High — approaching full.\n\n**Evidence**: `df -h` shows / at 94%.\n\n**Remediation**: Find large files with `du -h --max-depth=1 / 2>/dev/null | sort -rh | head -15`.",
        "disk"
    ))

    c1 = "call_19002"
    scenarios.append(build_example(
        "Check if nginx is running.",
        [
            (tc("bash", {"command": "systemctl status nginx --no-pager"}, c1),
             tr("● nginx.service - A high performance web server\n   Loaded: loaded (/lib/systemd/system/nginx.service; enabled)\n   Active: active (running) since Mon 2026-04-14 08:00:00 UTC; 6h ago\n   Main PID: 1234 (nginx)\n   Tasks: 5\n   Memory: 12.3M", c1)),
        ],
        "**Summary**: Nginx is running and healthy — active for 6 hours, using 12.3 MB memory.\n\n**Evidence**: `systemctl status nginx` shows active (running).",
        "service"
    ))

    c1 = "call_19003"
    scenarios.append(build_example(
        "Show what ports are open.",
        [
            (tc("bash", {"command": "ss -tlnp"}, c1),
             tr("State  Recv-Q Send-Q Local Address:Port  Peer Address:Port Process\nLISTEN 0      511    0.0.0.0:80     0.0.0.0:*     users:((\"nginx\",pid=1234,fd=6))\nLISTEN 0      511    0.0.0.0:443    0.0.0.0:*     users:((\"nginx\",pid=1234,fd=7))\nLISTEN 0      128    0.0.0.0:22     0.0.0.0:*     users:((\"sshd\",pid=890,fd=3))\nLISTEN 0      128    127.0.0.1:5432 0.0.0.0:*     users:((\"postgres\",pid=567,fd=5))", c1)),
        ],
        "**Summary**: Four ports open — nginx on 80/443, SSH on 22, PostgreSQL on 5432 (localhost only).\n\n**Evidence**: `ss -tlnp` shows nginx (80, 443), sshd (22), postgres (5432, bound to 127.0.0.1 only).\n\n**Risk Level**: Low — PostgreSQL correctly bound to localhost.",
        "port"
    ))

    return scenarios


def main():
    scenarios = generate_scenarios()

    # Shuffle for training
    random.seed(42)
    random.shuffle(scenarios)

    # Split: 90% train, 5% eval, 5% valid
    n = len(scenarios)
    n_eval = max(3, n // 20)
    n_valid = max(3, n // 20)
    n_train = n - n_eval - n_valid

    train = scenarios[:n_train]
    eval_data = scenarios[n_train:n_train + n_eval]
    valid = scenarios[n_train + n_eval:]

    output_dir = os.path.dirname(os.path.abspath(__file__))
    data_dir = os.path.join(output_dir, "data_v3")
    os.makedirs(data_dir, exist_ok=True)

    for name, data in [("train_realworld.jsonl", train), ("eval_realworld.jsonl", eval_data), ("valid_realworld.jsonl", valid)]:
        path = os.path.join(data_dir, name)
        with open(path, "w") as f:
            for item in data:
                f.write(json.dumps(item) + "\n")
        print(f"  {name}: {len(data)} examples")

    print(f"\nTotal: {n} examples")
    print(f"  Train: {n_train}, Eval: {n_eval}, Valid: {n_valid}")
    print(f"  Output: {data_dir}/")


if __name__ == "__main__":
    main()
