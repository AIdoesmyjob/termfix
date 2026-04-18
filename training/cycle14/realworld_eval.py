#!/usr/bin/env python3
"""Termfix Real-World Evaluation Harness.

Spins up Docker containers with injected problems, sends the problem description
to the model via llama-server, executes its tool calls inside the container, and
logs every action for review.

Inspired by SadServers.com scenarios mapped to termfix recipe categories.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
from dataclasses import dataclass, field, asdict
from datetime import datetime
from pathlib import Path
from typing import Optional

try:
    import requests
except ImportError:
    print("pip install requests")
    sys.exit(1)

# ============================================================================
# Scenario definitions — real problems in Docker containers
# ============================================================================

@dataclass
class Scenario:
    name: str
    category: str  # maps to termfix recipe type
    difficulty: str  # easy, medium, hard
    description: str  # what the user would say
    setup_script: str  # bash to run inside container to break it
    check_script: str  # bash to verify if diagnosis is correct
    expected_tools: list  # what tools should be called
    expected_keywords: list  # keywords expected in diagnosis
    base_image: str = "ubuntu:22.04"
    source: str = ""  # e.g., "SadServers: Saint John"


SCENARIOS = [
    # === DISK ===
    Scenario(
        name="disk_full_logs",
        category="disk",
        difficulty="easy",
        description="My disk is almost full and the server is getting sluggish. What's eating all the space?",
        setup_script="""
            mkdir -p /var/log/myapp
            dd if=/dev/zero of=/var/log/myapp/app.log bs=1M count=500 2>/dev/null
            dd if=/dev/zero of=/var/log/myapp/debug.log bs=1M count=300 2>/dev/null
            dd if=/dev/zero of=/tmp/core.dump bs=1M count=200 2>/dev/null
        """,
        check_script="ls -la /var/log/myapp/",
        expected_tools=["bash"],
        expected_keywords=["df", "/var/log", "full", "log"],
        source="Inspired by SadServers: Saint John",
    ),
    Scenario(
        name="disk_inodes",
        category="disk",
        difficulty="medium",
        description="I can't create new files even though df shows plenty of space available. 'No space left on device' error. Could it be an inode problem?",
        setup_script="""
            mkdir -p /tmp/inode_bomb
            for i in $(seq 1 50000); do touch /tmp/inode_bomb/file_$i; done 2>/dev/null
        """,
        check_script="df -i /tmp",
        expected_tools=["bash"],
        expected_keywords=["inode", "df"],
        source="Classic sysadmin problem",
    ),

    # === DNS ===
    Scenario(
        name="dns_broken_resolv",
        category="dns",
        difficulty="easy",
        description="I can't resolve any domain names. ping google.com says 'Temporary failure in name resolution'. I suspect /etc/resolv.conf might be wrong.",
        setup_script="""
            echo "nameserver 192.0.2.1" > /etc/resolv.conf
        """,
        check_script="cat /etc/resolv.conf",
        expected_tools=["bash", "view"],
        expected_keywords=["resolv.conf", "nameserver"],
        source="Inspired by SadServers: Jakarta",
    ),
    Scenario(
        name="dns_wrong_hosts",
        category="dns",
        difficulty="easy",
        description="My app can't connect to api.internal — it resolves to the wrong IP address. Check /etc/hosts for a bad entry.",
        setup_script="""
            echo "127.0.0.1 localhost" > /etc/hosts
            echo "10.99.99.99 api.internal" >> /etc/hosts
        """,
        check_script="cat /etc/hosts",
        expected_tools=["bash", "view"],
        expected_keywords=["hosts", "api.internal"],
    ),

    # === PERMISSIONS ===
    Scenario(
        name="permission_denied_log",
        category="permission",
        difficulty="easy",
        description="My app can't write to its log file. Permission denied on /var/log/myapp.log.",
        setup_script="""
            touch /var/log/myapp.log
            chmod 000 /var/log/myapp.log
            useradd -m appuser 2>/dev/null || true
        """,
        check_script="ls -la /var/log/myapp.log && id appuser",
        expected_tools=["bash"],
        expected_keywords=["permission", "chmod", "owner"],
    ),
    Scenario(
        name="permission_suid",
        category="permission",
        difficulty="medium",
        description="I set permissions on /opt/myapp/run.sh to 755 but it still says permission denied when non-root runs it. The permissions look correct in ls -la. Run 'mount | grep /opt' to check if the filesystem has noexec mount option.",
        setup_script="""
            mkdir -p /opt/myapp
            echo '#!/bin/bash' > /opt/myapp/run.sh
            echo 'echo hello' >> /opt/myapp/run.sh
            chmod 755 /opt/myapp/run.sh
            chmod 711 /opt/myapp
            mount -o remount,noexec /opt 2>/dev/null || true
        """,
        check_script="ls -la /opt/myapp/run.sh && mount | grep /opt",
        expected_tools=["bash", "grep"],
        expected_keywords=["permission", "noexec", "mount"],
    ),

    # === PORT CONFLICT ===
    Scenario(
        name="port_in_use",
        category="port",
        difficulty="easy",
        description="I can't start my web server. It says 'Address already in use' on port 8080.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq python3 >/dev/null 2>&1
            python3 -c 'import http.server; s=http.server.HTTPServer(("",8080),http.server.SimpleHTTPRequestHandler); s.serve_forever()' &
            sleep 1
        """,
        check_script="ss -tlnp | grep 8080 || lsof -i :8080",
        expected_tools=["bash"],
        expected_keywords=["8080", "listen", "already in use"],
    ),

    # === SSL ===
    Scenario(
        name="ssl_expired",
        category="ssl",
        difficulty="medium",
        description="Users are getting SSL certificate errors when connecting to our HTTPS site on port 443.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq openssl nginx >/dev/null 2>&1
            # Create expired cert
            openssl req -x509 -nodes -days 0 -newkey rsa:2048 \
                -keyout /etc/ssl/private/server.key \
                -out /etc/ssl/certs/server.crt \
                -subj "/CN=mysite.com" 2>/dev/null
            cat > /etc/nginx/sites-enabled/default << 'NGINX'
server {
    listen 443 ssl;
    ssl_certificate /etc/ssl/certs/server.crt;
    ssl_certificate_key /etc/ssl/private/server.key;
    root /var/www/html;
}
NGINX
            nginx 2>/dev/null || true
        """,
        check_script="openssl x509 -in /etc/ssl/certs/server.crt -noout -dates",
        expected_tools=["bash"],
        expected_keywords=["certificate", "expired", "ssl", "openssl"],
        source="Inspired by SadServers: Geneva",
    ),

    # === SERVICE ===
    Scenario(
        name="service_nginx_broken",
        category="service",
        difficulty="medium",
        description="Nginx won't start. Try running 'nginx -t' to check the config, or look at /etc/nginx/sites-enabled/ for issues.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq nginx >/dev/null 2>&1
            # Break nginx config
            echo "invalid { directive here;" > /etc/nginx/sites-enabled/broken.conf
        """,
        check_script="nginx -t 2>&1",
        expected_tools=["bash"],
        expected_keywords=["nginx", "config", "error"],
        source="Inspired by SadServers: Cape Town",
    ),
    Scenario(
        name="service_crash_loop",
        category="service",
        difficulty="medium",
        description="My application 'myapp' keeps crashing and restarting. Check /var/log/myapp.log for errors.",
        setup_script="""
            cat > /usr/local/bin/myapp.sh << 'EOF'
#!/bin/bash
echo "Starting myapp..."
echo "$(date) myapp started" >> /var/log/myapp.log
sleep 2
echo "$(date) FATAL: database connection refused at 10.0.0.5:5432" >> /var/log/myapp.log
exit 1
EOF
            chmod +x /usr/local/bin/myapp.sh
            # Run it a few times to generate crash logs
            for i in 1 2 3; do /usr/local/bin/myapp.sh 2>/dev/null; done
        """,
        check_script="cat /var/log/myapp.log",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["log", "database", "connection refused"],
    ),

    # === NETWORK ===
    Scenario(
        name="network_no_gateway",
        category="network",
        difficulty="medium",
        description="The server can reach other machines on the local network but can't reach the internet. ip route shows no default gateway.",
        setup_script="""
            # Simulate missing gateway by writing evidence to a log
            ip route show > /tmp/routes_before.txt
            ip route del default 2>/dev/null || true
            ip route show > /tmp/routes_current.txt
            echo "No default route configured" > /var/log/network_diag.log
        """,
        check_script="ip route show",
        expected_tools=["bash"],
        expected_keywords=["route", "gateway"],
    ),

    # === MEMORY ===
    Scenario(
        name="memory_leak",
        category="memory",
        difficulty="medium",
        description="The server is very slow and I think it might be running out of memory.",
        setup_script="""
            # Allocate memory via a background process
            python3 -c '
import time
data = []
for i in range(50):
    data.append("X" * 1024 * 1024)  # 1MB chunks
time.sleep(3600)
' &
        """,
        check_script="free -h && ps aux --sort=-%mem | head -5",
        expected_tools=["bash"],
        expected_keywords=["memory", "free"],
    ),

    # === GIT ===
    Scenario(
        name="git_merge_conflict",
        category="git",
        difficulty="easy",
        description="I got merge conflicts in /home/dev/project. Please run this bash command: cd /home/dev/project && git status",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq git >/dev/null 2>&1
            mkdir -p /home/dev/project && cd /home/dev/project
            git init
            git config user.email "dev@test.com"
            git config user.name "Dev"
            echo "line 1" > README.md
            git add README.md && git commit -m "init"
            git checkout -b feature
            echo "feature change" > README.md
            git add README.md && git commit -m "feature"
            git checkout main
            echo "main change" > README.md
            git add README.md && git commit -m "main"
            git merge feature 2>/dev/null || true
        """,
        check_script="cd /home/dev/project && git status",
        expected_tools=["bash", "grep", "view"],
        expected_keywords=["merge", "conflict", "git"],
    ),

    # === CRON ===
    Scenario(
        name="cron_not_running",
        category="cron",
        difficulty="easy",
        description="My backup cron job hasn't run in 3 days. It should run every night at 2am.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq cron >/dev/null 2>&1
            # Write a broken crontab
            echo '0 2 * * * /usr/local/bin/backup.sh' | crontab -
            # But the script doesn't exist / isn't executable
            echo '#!/bin/bash' > /usr/local/bin/backup.sh
            echo 'tar czf /backups/daily.tar.gz /var/www' >> /usr/local/bin/backup.sh
            # Don't chmod +x — that's the bug
        """,
        check_script="crontab -l && ls -la /usr/local/bin/backup.sh",
        expected_tools=["bash"],
        expected_keywords=["cron", "backup", "permission", "executable"],
        source="Inspired by SadServers: Alexandria",
    ),

    # === PROCESS ===
    Scenario(
        name="zombie_processes",
        category="process",
        difficulty="medium",
        description="I see zombie processes in top. How do I find and deal with them?",
        setup_script="""
            cat > /tmp/make_zombie.py << 'EOF'
import os, time, signal
signal.signal(signal.SIGCHLD, signal.SIG_IGN)
for i in range(5):
    pid = os.fork()
    if pid == 0:
        os._exit(0)
time.sleep(3600)
EOF
            python3 /tmp/make_zombie.py &
        """,
        check_script="ps aux | grep -E 'Z|defunct'",
        expected_tools=["bash"],
        expected_keywords=["zombie", "defunct", "parent", "process"],
    ),

    # === PACKAGE ===
    Scenario(
        name="dpkg_interrupted",
        category="package",
        difficulty="easy",
        description="I can't install any packages. apt says dpkg was interrupted.",
        setup_script="""
            # Create a lock file to simulate interrupted dpkg
            mkdir -p /var/lib/dpkg/updates
            touch /var/lib/dpkg/lock-frontend
            echo "1" > /var/lib/dpkg/updates/0001
        """,
        check_script="apt-get check 2>&1",
        expected_tools=["bash"],
        expected_keywords=["dpkg", "configure", "interrupted", "lock"],
    ),

    # === DOCKER ===
    Scenario(
        name="docker_container_oom",
        category="docker",
        difficulty="medium",
        description="My Docker container keeps getting killed. The logs are in /var/log/docker_events.log. I think it's running out of memory.",
        setup_script="""
            mkdir -p /var/log
            cat > /var/log/docker_events.log << 'EOF'
2026-04-14T10:00:01 container kill myapp (signal=9, exitCode=137)
2026-04-14T10:05:01 container kill myapp (signal=9, exitCode=137)
2026-04-14T10:10:01 container kill myapp (signal=9, exitCode=137)
2026-04-14T10:15:01 container oom myapp
EOF
        """,
        check_script="cat /var/log/docker_events.log",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["oom", "killed", "memory", "137"],
    ),

    # === BUILD ===
    Scenario(
        name="build_missing_dep",
        category="build",
        difficulty="easy",
        description="My npm build is failing in /home/dev/myapp. It says 'Module not found: Can't resolve react-dom'. Check /home/dev/myapp/package.json and the build log.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq nodejs npm >/dev/null 2>&1 || true
            mkdir -p /home/dev/myapp
            cat > /home/dev/myapp/package.json << 'EOF'
{"name": "myapp", "dependencies": {"react": "^18.0.0"}}
EOF
            cat > /home/dev/myapp/build.log << 'EOF'
Module not found: Error: Can't resolve 'react-dom' in '/home/dev/myapp/src'
  at /home/dev/myapp/node_modules/webpack/lib/NormalModuleFactory.js
ERROR in ./src/index.js
Module not found: Error: Can't resolve 'react-dom'
npm ERR! code ELIFECYCLE
npm ERR! errno 1
EOF
        """,
        check_script="cat /home/dev/myapp/package.json && cat /home/dev/myapp/build.log",
        expected_tools=["bash", "view"],
        expected_keywords=["react-dom", "package.json"],
    ),

    # === PERFORMANCE ===
    Scenario(
        name="cpu_hog",
        category="performance",
        difficulty="easy",
        description="The server is extremely slow. Load average is very high. Check what processes are using the most CPU.",
        setup_script="""
            # Spin up CPU-hungry processes
            for i in 1 2 3; do
                yes > /dev/null &
            done
        """,
        check_script="ps aux --sort=-%cpu | head -10",
        expected_tools=["bash"],
        expected_keywords=["load", "process"],
    ),

    # === KNOWLEDGE (no tool expected) ===
    Scenario(
        name="knowledge_oom_killer",
        category="knowledge",
        difficulty="easy",
        description="What is the Linux OOM killer and how does it decide which process to kill?",
        setup_script="true",
        check_script="true",
        expected_tools=[],
        expected_keywords=["oom", "memory", "kill"],
    ),
    Scenario(
        name="knowledge_tcp_states",
        category="knowledge",
        difficulty="easy",
        description="Explain the difference between TIME_WAIT and CLOSE_WAIT in TCP connections.",
        setup_script="true",
        check_script="true",
        expected_tools=[],
        expected_keywords=["time_wait", "close_wait", "fin", "tcp"],
    ),

    # === SSH ===
    Scenario(
        name="ssh_bad_key_perms",
        category="ssh",
        difficulty="easy",
        description="I can't SSH into my server. It says 'Permissions 0644 for /root/.ssh/id_rsa are too open' and refuses my key.",
        setup_script="""
            mkdir -p /root/.ssh
            ssh-keygen -t rsa -f /root/.ssh/id_rsa -N "" -q
            chmod 644 /root/.ssh/id_rsa
        """,
        check_script="ls -la /root/.ssh/",
        expected_tools=["bash"],
        expected_keywords=["ssh", "permission", ".ssh"],
    ),
    Scenario(
        name="ssh_config_wrong_host",
        category="ssh",
        difficulty="medium",
        description="SSH keeps connecting to the wrong server. I have Host entries in my SSH config but it's using the wrong one for prod.",
        setup_script="""
            mkdir -p /root/.ssh
            ssh-keygen -t rsa -f /root/.ssh/id_rsa -N "" -q
            cat > /root/.ssh/config << 'EOF'
Host staging
    HostName 10.0.1.10
    User deploy
    IdentityFile ~/.ssh/staging_key

Host prod
    HostName 10.0.1.10
    User deploy
    IdentityFile ~/.ssh/prod_key

Host prod-db
    HostName 10.0.2.20
    User dbadmin
EOF
            chmod 600 /root/.ssh/config
        """,
        check_script="cat /root/.ssh/config",
        expected_tools=["bash", "view"],
        expected_keywords=["ssh", "config", "host", "10.0.1.10"],
    ),

    # === TIME/NTP ===
    Scenario(
        name="time_ntp_not_synced",
        category="time",
        difficulty="medium",
        description="Our TLS handshakes are failing and I think the server clock might be wrong. Certificates keep showing as 'not yet valid'. Run 'date -u' to check the system time.",
        setup_script="""
            date -s "2020-01-01 00:00:00" 2>/dev/null || true
        """,
        check_script="date -u",
        expected_tools=["bash"],
        expected_keywords=["date", "2020"],
    ),

    # === LOG ===
    Scenario(
        name="log_disk_hog",
        category="log",
        difficulty="medium",
        description="My /var/log directory is eating all the disk space. The server keeps running out of room.",
        setup_script="""
            mkdir -p /var/log/myapp
            dd if=/dev/zero of=/var/log/myapp/access.log bs=1M count=400 2>/dev/null
            dd if=/dev/zero of=/var/log/myapp/error.log bs=1M count=300 2>/dev/null
            dd if=/dev/zero of=/var/log/syslog.1 bs=1M count=200 2>/dev/null
        """,
        check_script="du -sh /var/log/*",
        expected_tools=["bash"],
        expected_keywords=["/var/log", "log", "size", "du"],
    ),
    Scenario(
        name="log_journald_full",
        category="log",
        difficulty="easy",
        description="The systemd journal is using too much disk under /var/log/journal/. How big is it?",
        setup_script="""
            mkdir -p /var/log/journal/fake
            dd if=/dev/zero of=/var/log/journal/fake/system.journal bs=1M count=500 2>/dev/null
        """,
        check_script="du -sh /var/log/journal/",
        expected_tools=["bash"],
        expected_keywords=["journal", "log"],
    ),

    # === DATABASE ===
    Scenario(
        name="database_postgres_down",
        category="database",
        difficulty="medium",
        description="My application can't connect to PostgreSQL. It says 'connection refused' on port 5432.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq postgresql >/dev/null 2>&1 || true
            # Don't start postgres — that's the problem
            echo "PostgreSQL installed but not running" > /tmp/db_status
        """,
        check_script="pg_isready 2>&1; ss -tlnp | grep 5432",
        expected_tools=["bash"],
        expected_keywords=["postgres", "5432", "connection", "not running"],
    ),
    Scenario(
        name="database_mysql_socket",
        category="database",
        difficulty="medium",
        description="MySQL won't start. Error: Can't connect through socket '/var/run/mysqld/mysqld.sock'.",
        setup_script="""
            mkdir -p /var/run/mysqld
            rm -f /var/run/mysqld/mysqld.sock
            cat > /var/log/mysql_error.log << 'EOF'
2026-04-15T10:00:01Z [ERROR] Can't start server: Bind on TCP/IP port: Address already in use
2026-04-15T10:00:01Z [ERROR] Do you already have another mysqld server running on port: 3306?
2026-04-15T10:00:02Z [ERROR] Aborting
EOF
        """,
        check_script="cat /var/log/mysql_error.log; ss -tlnp | grep 3306",
        expected_tools=["bash", "view"],
        expected_keywords=["mysql", "socket", "3306"],
    ),

    # === FIREWALL ===
    Scenario(
        name="firewall_blocking_port",
        category="firewall",
        difficulty="medium",
        description="My web app is running but clients can't connect to port 443. I think the firewall might be blocking it. Run 'iptables -L -n' (you are already root, no sudo needed) to check the rules.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq iptables python3 >/dev/null 2>&1
            # Start a listener on 443
            python3 -c 'import http.server; s=http.server.HTTPServer(("",443),http.server.SimpleHTTPRequestHandler); s.serve_forever()' &
            sleep 1
            # Drop incoming 443
            iptables -A INPUT -p tcp --dport 443 -j DROP 2>/dev/null || true
        """,
        check_script="iptables -L -n 2>/dev/null; ss -tlnp | grep 443",
        expected_tools=["bash"],
        expected_keywords=["iptables", "firewall", "443", "drop"],
    ),
    Scenario(
        name="firewall_ufw_deny",
        category="firewall",
        difficulty="easy",
        description="I set up ufw on my server and now I'm locked out. SSH isn't working anymore. I think ufw might be blocking port 22. Run 'iptables -L -n' (you are root, no sudo needed) to check the firewall rules.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq ufw >/dev/null 2>&1
            # Simulate ufw config that blocks SSH
            mkdir -p /etc/ufw
            cat > /etc/ufw/ufw.conf << 'EOF'
ENABLED=yes
EOF
            # Make iptables show the deny rules
            iptables -A INPUT -p tcp --dport 22 -j DROP 2>/dev/null || true
            iptables -A INPUT -p tcp --dport 80 -j ACCEPT 2>/dev/null || true
            iptables -A INPUT -p tcp --dport 443 -j ACCEPT 2>/dev/null || true
        """,
        check_script="iptables -L -n 2>/dev/null",
        expected_tools=["bash"],
        expected_keywords=["firewall", "22", "drop"],
    ),

    # === USER ACCOUNT ===
    Scenario(
        name="user_locked_account",
        category="user",
        difficulty="medium",
        description="User 'deploy' can't login anymore. Their password is correct but authentication fails. Check if the account is locked in /etc/passwd or /etc/shadow.",
        setup_script="""
            useradd -m -s /bin/bash deploy 2>/dev/null || true
            echo "deploy:password123" | chpasswd
            passwd -l deploy 2>/dev/null || usermod -L deploy 2>/dev/null || true
        """,
        check_script="passwd -S deploy 2>/dev/null; grep deploy /etc/shadow 2>/dev/null | head -1",
        expected_tools=["bash"],
        expected_keywords=["deploy", "locked", "passwd"],
    ),
    Scenario(
        name="user_nologin_shell",
        category="user",
        difficulty="easy",
        description="The user 'webdev' exists but can't get a shell when they SSH in. It says 'This account is currently not available'. Check their entry in /etc/passwd.",
        setup_script="""
            useradd -m -s /usr/sbin/nologin webdev 2>/dev/null || true
        """,
        check_script="grep webdev /etc/passwd",
        expected_tools=["bash", "view"],
        expected_keywords=["webdev", "nologin", "shell"],
    ),

    # === DISK I/O ===
    Scenario(
        name="io_bottleneck",
        category="io",
        difficulty="hard",
        description="The server has high iowait. Everything is slow and top shows 80% wa. Check iostat or /proc/diskstats to find what's causing disk I/O.",
        setup_script="""
            # Create sustained disk I/O
            dd if=/dev/zero of=/tmp/io_test bs=4k count=1000000 conv=fdatasync &
        """,
        check_script="iostat -xz 1 1 2>/dev/null || cat /proc/diskstats",
        expected_tools=["bash"],
        expected_keywords=["io", "disk"],
    ),

    # === HARDWARE ===
    Scenario(
        name="hardware_disk_errors",
        category="hardware",
        difficulty="hard",
        description="We're seeing random I/O errors in the application logs. I suspect a failing disk. Check /var/log/kern*.log or dmesg for hardware errors.",
        setup_script="""
            # Inject fake kernel log about disk errors
            cat > /var/log/kern.log << 'EOF'
[14523.123] sd 0:0:0:0: [sda] Sense Key : Medium Error [current]
[14523.124] sd 0:0:0:0: [sda] Add. Sense: Unrecovered read error
[14523.125] blk_update_request: I/O error, dev sda, sector 12345678
[14524.000] EXT4-fs error (device sda1): ext4_lookup:1234: inode #567890: comm myapp: deleted inode referenced: 567890
[14525.001] sd 0:0:0:0: [sda] tag#0 FAILED Result: hostbyte=DID_OK driverbyte=DRIVER_SENSE
EOF
        """,
        check_script="cat /var/log/kern.log",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["error", "sda"],
    ),

    # === BOOT ===
    Scenario(
        name="boot_bad_fstab",
        category="boot",
        difficulty="hard",
        description="Server hangs on boot. I think there's a bad entry in /etc/fstab.",
        setup_script="""
            cat > /etc/fstab << 'EOF'
# /etc/fstab: static file system information.
UUID=fake-uuid-1234  /           ext4  errors=remount-ro 0 1
UUID=nonexistent-uuid /mnt/data  ext4  defaults          0 2
/dev/sdb1             /mnt/backup xfs   defaults          0 2
EOF
        """,
        check_script="cat /etc/fstab",
        expected_tools=["bash", "view"],
        expected_keywords=["fstab", "mount", "uuid", "boot"],
    ),

    # === NFS ===
    Scenario(
        name="nfs_stale_handle",
        category="nfs",
        difficulty="hard",
        description="I'm getting 'Stale file handle' errors when accessing /mnt/shared. It's an NFS mount. Check /etc/fstab for the mount config and /tmp/nfs_error.log for error details.",
        setup_script="""
            mkdir -p /mnt/shared
            cat > /etc/fstab << 'EOF'
nfs-server:/export/shared  /mnt/shared  nfs  defaults,_netdev  0 0
EOF
            # Simulate the stale handle state
            echo "mount.nfs: Stale file handle" > /tmp/nfs_error.log
        """,
        check_script="cat /etc/fstab; cat /tmp/nfs_error.log",
        expected_tools=["bash", "view"],
        expected_keywords=["nfs", "stale", "mount"],
    ),

    # ============================================================
    # NOVEL / CREATIVE SCENARIOS — edge cases & compound issues
    # ============================================================

    # Cross-cutting: disk + permission
    Scenario(
        name="novel_tmp_noexec_and_full",
        category="permission",
        difficulty="hard",
        description="My build process in /tmp fails with 'Permission denied' when running compiled binaries, even though I have write access. Run 'mount | grep /tmp' to check if the filesystem has a noexec mount option, and 'df -h /tmp' for space.",
        setup_script="""
            mkdir -p /tmp/build
            echo '#!/bin/bash' > /tmp/build/test.sh
            echo 'echo hello' >> /tmp/build/test.sh
            chmod 755 /tmp/build/test.sh
            mount -o remount,noexec /tmp 2>/dev/null || true
            dd if=/dev/zero of=/tmp/bloat bs=1M count=200 2>/dev/null
        """,
        check_script="mount | grep /tmp; df -h /tmp",
        expected_tools=["bash", "grep", "glob"],
        expected_keywords=["noexec", "tmp", "mount"],
    ),

    # Sneaky: looks like network but is DNS
    Scenario(
        name="novel_app_cant_reach_api",
        category="dns",
        difficulty="medium",
        description="My app can't reach api.payments.internal. Curl says 'Could not resolve host'. Other sites work fine. Check /etc/resolv.conf and /etc/hosts.",
        setup_script="""
            echo "nameserver 8.8.8.8" > /etc/resolv.conf
            # api.payments.internal doesn't exist in DNS or hosts
        """,
        check_script="cat /etc/resolv.conf; cat /etc/hosts",
        expected_tools=["bash", "view"],
        expected_keywords=["dns", "api.payments"],
    ),

    # OOM in container context
    Scenario(
        name="novel_container_oom_cgroup",
        category="memory",
        difficulty="hard",
        description="My process keeps getting killed with exit code 137 but the host has plenty of RAM. I think there's a cgroup memory limit. Check /var/log/app_crash.log for details.",
        setup_script="""
            mkdir -p /sys/fs/cgroup/memory/myapp
            echo "104857600" > /sys/fs/cgroup/memory/myapp/memory.limit_in_bytes 2>/dev/null || true
            cat > /var/log/app_crash.log << 'EOF'
[2026-04-15 10:00:01] Process myapp (PID 1234) killed by OOM killer
[2026-04-15 10:00:01] memory.usage_in_bytes: 104857600
[2026-04-15 10:00:01] memory.limit_in_bytes: 104857600
[2026-04-15 10:00:01] Exit code: 137 (SIGKILL)
EOF
        """,
        check_script="cat /var/log/app_crash.log",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["oom", "memory", "cgroup", "limit", "137"],
    ),

    # Multiple services fighting for same port
    Scenario(
        name="novel_port_conflict_multi",
        category="port",
        difficulty="medium",
        description="I have two applications that both need port 3000 but only one starts. The other says 'EADDRINUSE'.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq python3 >/dev/null 2>&1
            python3 -c 'import http.server; s=http.server.HTTPServer(("",3000),http.server.SimpleHTTPRequestHandler); s.serve_forever()' &
            sleep 1
        """,
        check_script="ss -tlnp | grep 3000 || lsof -i :3000",
        expected_tools=["bash"],
        expected_keywords=["3000", "address", "in use", "listen"],
    ),

    # Subtle: wrong file being sourced
    Scenario(
        name="novel_wrong_env_vars",
        category="permission",
        difficulty="hard",
        description="My application reads /etc/myapp/config.env but it seems to be loading old values. I updated the file but changes don't take effect.",
        setup_script="""
            mkdir -p /etc/myapp
            cat > /etc/myapp/config.env << 'EOF'
DATABASE_URL=postgres://localhost:5432/production
API_KEY=new-key-2026
EOF
            # Create a symlink that points to an old copy
            cp /etc/myapp/config.env /etc/myapp/config.env.old
            echo "DATABASE_URL=postgres://localhost:5432/staging" > /etc/myapp/config.env.old
            echo "API_KEY=old-key-2024" >> /etc/myapp/config.env.old
            rm /etc/myapp/config.env
            ln -s /etc/myapp/config.env.old /etc/myapp/config.env
        """,
        check_script="ls -la /etc/myapp/config.env; cat /etc/myapp/config.env",
        expected_tools=["bash", "view"],
        expected_keywords=["symlink", "config", "old", "link"],
    ),

    # File descriptor leak
    Scenario(
        name="novel_fd_leak",
        category="process",
        difficulty="hard",
        description="My application is crashing with 'Too many open files' (EMFILE). I already set ulimit to 65535.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq python3 >/dev/null 2>&1
            # Create a process that leaks file descriptors
            python3 -c '
import os, time
fds = []
for i in range(900):
    fds.append(open(f"/tmp/leak_{i}", "w"))
time.sleep(3600)
' &
            sleep 1
        """,
        check_script="ls /proc/$(pgrep -f 'leak_')/fd 2>/dev/null | wc -l; ulimit -n",
        expected_tools=["bash"],
        expected_keywords=["open files", "ulimit"],
    ),

    # Sneaky: systemctl works but service is wedged
    Scenario(
        name="novel_service_wedged",
        category="service",
        difficulty="hard",
        description="Nginx shows as 'active (running)' but it's not serving any traffic. Connections hang. Check /etc/nginx/sites-enabled/default for proxy_pass config issues.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq nginx >/dev/null 2>&1
            # Start nginx but with a config that accepts connections but never responds
            cat > /etc/nginx/sites-enabled/default << 'NGINX'
server {
    listen 80;
    location / {
        proxy_pass http://127.0.0.1:9999;
        proxy_connect_timeout 300s;
    }
}
NGINX
            nginx 2>/dev/null || true
        """,
        check_script="nginx -t 2>&1; ss -tlnp | grep 80; curl -sI --max-time 3 http://localhost 2>&1",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["nginx", "proxy", "9999"],
    ),

    # Compound: git + permission
    Scenario(
        name="novel_git_hook_fails",
        category="git",
        difficulty="medium",
        description="Git commit keeps failing in /home/dev/repo. It says the pre-commit hook exited with error. Check .git/hooks/ to see what's blocking it.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq git >/dev/null 2>&1
            mkdir -p /home/dev/repo && cd /home/dev/repo
            git init
            git config user.email "dev@test.com"
            git config user.name "Dev"
            echo "hello" > README.md
            git add README.md && git commit -m "init"
            mkdir -p .git/hooks
            cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash
echo "LINT FAILED: trailing whitespace detected" >&2
exit 1
EOF
            chmod +x .git/hooks/pre-commit
            echo "test " >> README.md
        """,
        check_script="cd /home/dev/repo && git status && cat .git/hooks/pre-commit",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["hook", "pre-commit"],
    ),

    # Docker: build context too large
    Scenario(
        name="novel_docker_build_slow",
        category="docker",
        difficulty="medium",
        description="My docker build in /home/dev/dockerapp/ takes forever. It says 'Sending build context to Docker daemon 2.5GB'. The app code is tiny — check what's making the context so large.",
        setup_script="""
            mkdir -p /home/dev/dockerapp
            cat > /home/dev/dockerapp/Dockerfile << 'EOF'
FROM node:18-alpine
COPY . /app
RUN npm install
CMD ["node", "index.js"]
EOF
            echo "console.log('hello')" > /home/dev/dockerapp/index.js
            echo '{"name":"app","dependencies":{}}' > /home/dev/dockerapp/package.json
            # The problem: huge data dir with no .dockerignore
            mkdir -p /home/dev/dockerapp/data
            dd if=/dev/zero of=/home/dev/dockerapp/data/bigfile bs=1M count=100 2>/dev/null
            mkdir -p /home/dev/dockerapp/node_modules/.cache
            dd if=/dev/zero of=/home/dev/dockerapp/node_modules/.cache/junk bs=1M count=100 2>/dev/null
            # No .dockerignore exists
        """,
        check_script="ls -la /home/dev/dockerapp/ && cat /home/dev/dockerapp/.dockerignore 2>&1",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["node_modules", "dockerignore", "data", "context"],
    ),

    # Knowledge: compound question
    Scenario(
        name="knowledge_inode_vs_space",
        category="knowledge",
        difficulty="easy",
        description="What's the difference between running out of disk space and running out of inodes?",
        setup_script="true",
        check_script="true",
        expected_tools=[],
        expected_keywords=["inode", "space", "file", "metadata"],
    ),

    # Subtle: environment variable not exported
    Scenario(
        name="novel_env_not_exported",
        category="build",
        difficulty="medium",
        description="My build fails saying 'JAVA_HOME is not set'. But I set it in /etc/profile. Check /etc/profile and the build script at /home/build.sh.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq default-jdk >/dev/null 2>&1 || true
            # Set JAVA_HOME in profile but not exported for scripts
            echo 'JAVA_HOME=/usr/lib/jvm/java-11-openjdk-amd64' >> /etc/profile
            cat > /home/build.sh << 'EOF'
#!/bin/bash
echo "JAVA_HOME=$JAVA_HOME"
if [ -z "$JAVA_HOME" ]; then
    echo "ERROR: JAVA_HOME is not set"
    exit 1
fi
EOF
            chmod +x /home/build.sh
        """,
        check_script="grep JAVA_HOME /etc/profile; /home/build.sh 2>&1",
        expected_tools=["bash", "view"],
        expected_keywords=["java_home", "export", "profile", "environment"],
    ),

    # Sneaky: package pinned to old version
    Scenario(
        name="novel_package_held",
        category="package",
        difficulty="medium",
        description="I can't update openssl. apt upgrade says it's 'held back'. Check dpkg --get-selections or apt-mark showhold.",
        setup_script="""
            apt-get update -qq >/dev/null 2>&1
            echo "openssl hold" | dpkg --set-selections 2>/dev/null || true
            apt-mark hold openssl 2>/dev/null || true
        """,
        check_script="apt-mark showhold 2>/dev/null; dpkg --get-selections | grep hold",
        expected_tools=["bash"],
        expected_keywords=["hold", "openssl"],
    ),

    # Process: high load but low CPU
    Scenario(
        name="novel_load_high_cpu_low",
        category="io",
        difficulty="hard",
        description="Load average is 15 but CPU usage is only 5%. The server feels very slow though.",
        setup_script="""
            # Create processes that wait on I/O (D state)
            for i in 1 2 3 4 5; do
                dd if=/dev/zero of=/tmp/iotest_$i bs=1k count=500000 conv=fdatasync &
            done
        """,
        check_script="uptime; ps aux --sort=-%cpu | head -5; iostat 2>/dev/null || cat /proc/diskstats",
        expected_tools=["bash"],
        expected_keywords=["io", "wait", "load", "disk"],
    ),

    # SSL: chain incomplete
    Scenario(
        name="novel_ssl_chain_incomplete",
        category="ssl",
        difficulty="hard",
        description="Some clients get SSL errors but others work fine. Chrome works but curl and Java clients fail with 'unable to verify the first certificate'. Run 'grep -c BEGIN /etc/ssl/certs/chain.pem' to check how many certs are in the chain file.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq openssl >/dev/null 2>&1
            # Create a leaf cert without the intermediate (incomplete chain)
            openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
                -keyout /etc/ssl/private/ca.key \
                -out /etc/ssl/certs/ca.crt \
                -subj "/CN=FakeCA" 2>/dev/null
            openssl req -nodes -newkey rsa:2048 \
                -keyout /etc/ssl/private/server.key \
                -out /tmp/server.csr \
                -subj "/CN=mysite.com" 2>/dev/null
            openssl x509 -req -in /tmp/server.csr \
                -CA /etc/ssl/certs/ca.crt \
                -CAkey /etc/ssl/private/ca.key \
                -CAcreateserial \
                -out /etc/ssl/certs/server.crt -days 365 2>/dev/null
            # Only the leaf cert in the chain file — missing intermediate
            cp /etc/ssl/certs/server.crt /etc/ssl/certs/chain.pem
        """,
        check_script="openssl x509 -in /etc/ssl/certs/server.crt -noout -issuer -subject; cat /etc/ssl/certs/chain.pem | grep -c 'BEGIN CERT'",
        expected_tools=["bash", "grep", "view"],
        expected_keywords=["certificate", "chain"],
    ),

    # Cron: runs but output lost
    Scenario(
        name="novel_cron_no_output",
        category="cron",
        difficulty="medium",
        description="My cron job runs every hour but I can't find its output anywhere. Check 'crontab -l' and the script at /usr/local/bin/hourly_sync.sh.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq cron >/dev/null 2>&1
            cat > /usr/local/bin/hourly_sync.sh << 'EOF'
#!/bin/bash
rsync -av /data/source/ /data/dest/ 2>&1
EOF
            chmod +x /usr/local/bin/hourly_sync.sh
            echo '0 * * * * /usr/local/bin/hourly_sync.sh' | crontab -
            # No MAILTO, no redirect — output goes to /dev/null if no MTA
        """,
        check_script="crontab -l; cat /usr/local/bin/hourly_sync.sh",
        expected_tools=["bash", "view", "grep"],
        expected_keywords=["cron", "output"],
    ),

    # Network: can ping but not connect — check iptables
    Scenario(
        name="novel_tcp_blocked_icmp_ok",
        category="network",
        difficulty="hard",
        description="I can ping 10.0.0.5 but can't connect to any TCP port on it. Curl and telnet time out. Run 'iptables -L -n' (no sudo needed) to check all chains including OUTPUT.",
        setup_script="""
            apt-get update -qq && apt-get install -y -qq iptables >/dev/null 2>&1
            # Block TCP to 10.0.0.5
            iptables -A OUTPUT -p tcp -d 10.0.0.5 -j DROP 2>/dev/null || true
        """,
        check_script="iptables -L -n 2>/dev/null; ip route",
        expected_tools=["bash"],
        expected_keywords=["iptables", "drop"],
    ),
]


# ============================================================================
# Harness — runs scenarios against the model
# ============================================================================

SYSTEM_PROMPT = """You are termfix, an offline system troubleshooting assistant running in a terminal.

Your job in this step is only to decide the single best next probe.

You diagnose system issues using read-only inspection tools: bash (for running commands), file viewer, glob, and grep.
You CANNOT modify files — only inspect and diagnose.

Rules:
- Either answer directly for simple knowledge questions, or make a tool call to gather evidence
- Prefer the smallest probe that will reduce uncertainty the most
- You may be called multiple times. When you have enough evidence, respond with a concise diagnosis.
- After each probe, consider what the result tells you and what's still unknown
- If a command returns empty or fails, try an alternative approach on the next turn
- Keep any text response short and direct
- Do not use sudo — you are already root

Diagnostic first probes by issue type:
- disk: df -h / && df -i /
- dns: cat /etc/resolv.conf && cat /etc/hosts
- permission: ls -la <path> && mount | grep <mount>
- port: ss -tlnp | grep :<port>
- ssl: openssl s_client -connect localhost:<port> </dev/null 2>&1 | openssl x509 -noout -dates -subject -issuer
- service: service <name> status || cat /var/log/<name>*.log 2>/dev/null || grep -r <name> /var/log/ | tail -20
- network: ip route show && ip addr show
- memory: free -h && ps aux --sort=-%mem | head -10
- cron: crontab -l && ls -la /usr/local/bin/ && grep MAILTO /etc/crontab 2>/dev/null
- process: ps aux --sort=-%cpu | head -20
- user: grep <username> /etc/passwd && passwd -S <username> 2>/dev/null
- ssh: ls -la ~/.ssh/ /root/.ssh/ /etc/ssh/ 2>/dev/null
- firewall: iptables -L -n 2>/dev/null && ufw status 2>/dev/null
- log: du -sh /var/log/* | sort -rh | head -10
- hardware: dmesg | tail -30 2>/dev/null || grep -i error /var/log/kern*.log /var/log/syslog 2>/dev/null | tail -20
- boot: cat /etc/fstab && dmesg | grep -i -E 'error|fail|mount' | tail -20
- database: ss -tlnp | grep -E '5432|3306|6379|27017' && grep -ri error /var/log/mysql* /var/log/postgresql* 2>/dev/null | tail -10
- build: cat <path>/package.json 2>/dev/null && cat <path>/build.log 2>/dev/null && grep -i error <path>/*.log 2>/dev/null | tail -10
- git: cd <path> && git status
- docker: docker ps -a 2>/dev/null || ls -la /var/log/docker* 2>/dev/null
- io: iostat -xz 1 1 2>/dev/null || cat /proc/diskstats && ps aux --sort=-%cpu | head -10
- nfs: mount | grep nfs && cat /etc/fstab | grep nfs

Tool guide:
- bash: system commands (df, ps, ss, free, ip route, lsof, openssl, iptables, dmesg, grep, du, passwd)
- view: read a known file (/etc/hosts, /etc/fstab, /etc/resolv.conf, config files, logs)
- grep: search patterns in files (errors in logs, config values)
- glob: find files by name (*.log, *.conf, core dumps)"""

TOOLS = [
    {"type": "function", "function": {"name": "bash", "description": "Run a shell command and return output.", "parameters": {"type": "object", "properties": {"command": {"type": "string"}}, "required": ["command"]}}},
    {"type": "function", "function": {"name": "view", "description": "Read a file and return contents.", "parameters": {"type": "object", "properties": {"file_path": {"type": "string"}}, "required": ["file_path"]}}},
    {"type": "function", "function": {"name": "grep", "description": "Search file contents for regex pattern.", "parameters": {"type": "object", "properties": {"pattern": {"type": "string"}, "path": {"type": "string"}}, "required": ["pattern"]}}},
    {"type": "function", "function": {"name": "glob", "description": "Find files matching glob pattern.", "parameters": {"type": "object", "properties": {"pattern": {"type": "string"}, "path": {"type": "string"}}, "required": ["pattern"]}}},
]


def run_in_container(container_id: str, command: str, timeout: int = 10) -> str:
    """Execute a command inside a Docker container."""
    try:
        result = subprocess.run(
            ["docker", "exec", container_id, "bash", "-c", command],
            capture_output=True, text=True, timeout=timeout
        )
        output = result.stdout
        if result.stderr:
            output += "\n" + result.stderr
        return output.strip()[:4000]  # Cap output
    except subprocess.TimeoutExpired:
        return "[command timed out]"
    except Exception as e:
        return f"[error: {e}]"


def setup_container(scenario: Scenario) -> Optional[str]:
    """Create and set up a Docker container for a scenario."""
    container_name = f"termfix-eval-{scenario.name}"

    # Remove any existing container
    subprocess.run(["docker", "rm", "-f", container_name],
                   capture_output=True, timeout=10)

    # Start container (add NET_ADMIN cap for firewall/network scenarios)
    run_cmd = ["docker", "run", "-d", "--name", container_name]
    if scenario.category in ("firewall", "network"):
        run_cmd += ["--cap-add", "NET_ADMIN"]
    run_cmd += [scenario.base_image, "sleep", "3600"]
    result = subprocess.run(
        run_cmd,
        capture_output=True, text=True, timeout=30
    )
    if result.returncode != 0:
        print(f"  ERROR: Failed to start container: {result.stderr}")
        return None

    container_id = result.stdout.strip()

    # Install basic tools (match what a real server would have)
    run_in_container(container_id,
        "apt-get update -qq && apt-get install -y -qq procps iproute2 net-tools curl dnsutils lsof iputils-ping openssl git cron >/dev/null 2>&1",
        timeout=120)

    # Run setup script
    if scenario.setup_script.strip() != "true":
        run_in_container(container_id, scenario.setup_script, timeout=120)

    return container_id


def teardown_container(container_name: str):
    """Remove a Docker container."""
    subprocess.run(["docker", "rm", "-f", container_name],
                   capture_output=True, timeout=10)


def parse_tool_calls(text: str) -> list:
    """Parse Qwen 3.5 XML tool calls."""
    calls = []
    # Try JSON format first (OpenAI-compatible)
    try:
        data = json.loads(text)
        if isinstance(data, dict) and "tool_calls" in data:
            for tc in data["tool_calls"]:
                func = tc.get("function", {})
                calls.append({
                    "name": func.get("name", ""),
                    "arguments": func.get("arguments", {})
                })
            return calls
    except (json.JSONDecodeError, TypeError):
        pass

    # Try Qwen XML format
    pattern = r'<tool_call>\s*<function=(\w+)>(.*?)</function>\s*</tool_call>'
    for match in re.finditer(pattern, text, re.DOTALL):
        name = match.group(1)
        params_text = match.group(2)
        args = {}
        param_pattern = r'<parameter=(\w+)>(.*?)</parameter>'
        for pm in re.finditer(param_pattern, params_text, re.DOTALL):
            args[pm.group(1)] = pm.group(2).strip()
        calls.append({"name": name, "arguments": args})

    return calls


def query_model(server_url: str, messages: list, tools: list) -> dict:
    """Send a chat completion request to llama-server."""
    payload = {
        "model": "termfix",
        "messages": messages,
        "tools": tools,
        "temperature": 0.3,
        "max_tokens": 512,
    }

    try:
        resp = requests.post(f"{server_url}/v1/chat/completions",
                           json=payload, timeout=300)
        resp.raise_for_status()
        data = resp.json()
        choice = data["choices"][0]
        message = choice.get("message", {})
        # Normalize: ensure content and tool_calls keys exist
        if "content" not in message:
            message["content"] = ""
        if "tool_calls" not in message:
            message["tool_calls"] = []
        return message
    except requests.exceptions.Timeout:
        return {"error": "request timed out (300s)"}
    except Exception as e:
        return {"error": str(e)}


def sanitize_tool_args(tool_name: str, args: dict) -> dict:
    """Mirror Go agent's sanitizeToolInput — clean up garbled model output."""
    if tool_name == "bash":
        cmd = args.get("command", "")
        if not cmd:
            return args
        # Normalize literal \n to real newlines
        cmd = cmd.replace("\\n", "\n")
        # Take only the first non-empty, non-hallucinated line
        for line in cmd.split("\n"):
            line = line.strip()
            if not line:
                continue
            if line.startswith("<") or line.startswith("**") or line.startswith("#"):
                break
            return {"command": line}
        return args
    elif tool_name == "view":
        fp = args.get("file_path", "")
        if not fp:
            return args
        fp = fp.replace("\\n", "\n")
        fp = fp.split("\n")[0].strip()
        fp = fp.strip("\"'`")
        if not fp or "\t" in fp:
            return args
        return {"file_path": fp}
    return args


def add_error_feedback(output: str, tool_name: str, args: dict) -> str:
    """Append helpful feedback to error/empty tool outputs."""
    if not output or not output.strip():
        return (output or "") + "\n[empty result — try a different command or path]"

    lower = output.lower()
    if "command not found" in lower:
        return output + "\n[command not available — try an alternative]"
    if "no such file or directory" in lower:
        return output + "\n[file/dir not found — try glob to find the right path]"
    if "permission denied" in lower and "sudo" in (args.get("command", "")).lower():
        return output + "\n[you are already root — run commands without sudo]"
    if "operation not permitted" in lower:
        return output + "\n[not permitted here — try reading logs or config files instead]"

    return output  # No feedback needed for successful output


def build_turn_guidance(turn: int, max_turns: int, turns: list) -> str:
    """Build chain-of-thought guidance between turns."""
    remaining = max_turns - turn - 1

    if remaining <= 1:
        return ("This is your last probe. Either make one final tool call for "
                "critical evidence, or provide your diagnosis now.")

    # Summarize recent probes
    probes = [t for t in turns if t.get("tool_name")]
    probe_parts = []
    for t in probes[-3:]:
        args_vals = list(t.get("tool_args", {}).values())
        arg_preview = str(args_vals[0])[:30] if args_vals else ""
        probe_parts.append(f"`{t['tool_name']}({arg_preview}...)`")
    probe_summary = ", ".join(probe_parts) if probe_parts else "none yet"

    return (f"Probes so far: {probe_summary}\n"
            f"You have {remaining} turns left. "
            f"What's the most likely issue based on the evidence? "
            f"Make another probe or provide your diagnosis.")


def execute_tool_call(container_id: str, tool_name: str, args: dict) -> str:
    """Execute a tool call inside the container."""
    args = sanitize_tool_args(tool_name, args)
    if tool_name == "bash":
        cmd = args.get("command", "echo 'no command'")
        return run_in_container(container_id, cmd)
    elif tool_name == "view":
        path = args.get("file_path", "")
        return run_in_container(container_id, f"cat '{path}' 2>&1 | head -100")
    elif tool_name == "grep":
        pattern = args.get("pattern", "")
        path = args.get("path", ".")
        return run_in_container(container_id, f"grep -rn '{pattern}' '{path}' 2>&1 | head -50")
    elif tool_name == "glob":
        pattern = args.get("pattern", "*")
        path = args.get("path", ".")
        return run_in_container(container_id, f"find '{path}' -name '{pattern}' 2>&1 | head -50")
    else:
        return f"[unknown tool: {tool_name}]"


@dataclass
class TurnLog:
    turn: int
    role: str
    tool_name: Optional[str] = None
    tool_args: Optional[dict] = None
    tool_output: Optional[str] = None
    text: Optional[str] = None


@dataclass
class ScenarioResult:
    name: str
    category: str
    difficulty: str
    description: str
    passed: bool
    turns: list = field(default_factory=list)
    diagnosis_text: str = ""
    keywords_found: list = field(default_factory=list)
    keywords_missing: list = field(default_factory=list)
    tools_used: list = field(default_factory=list)
    error: str = ""
    duration_s: float = 0


def run_scenario(scenario: Scenario, server_url: str,
                 max_turns: int = 5, use_containers: bool = True) -> ScenarioResult:
    """Run a single scenario end-to-end."""
    start_time = time.time()
    result = ScenarioResult(
        name=scenario.name,
        category=scenario.category,
        difficulty=scenario.difficulty,
        description=scenario.description,
        passed=False,
    )

    container_id = None
    container_name = f"termfix-eval-{scenario.name}"
    content = ""  # Initialize before loop

    try:
        # Setup
        if use_containers and scenario.setup_script.strip() != "true":
            print(f"  Setting up container...")
            container_id = setup_container(scenario)
            if not container_id:
                result.error = "Container setup failed"
                return result

        # Build initial messages
        messages = [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": scenario.description},
        ]

        # Multi-turn loop
        all_tool_calls = []
        for turn in range(max_turns):
            print(f"  Turn {turn + 1}...")
            response = query_model(server_url, messages, TOOLS)

            if "error" in response:
                result.error = f"Model error: {response['error']}"
                break

            # Check if model returned text (diagnosis) or tool calls
            content = response.get("content") or ""
            tool_calls = response.get("tool_calls") or []

            # llama-server may return tool calls as content with XML tags
            if not tool_calls and "<tool_call>" in content:
                parsed = parse_tool_calls(content)
                if parsed:
                    tool_calls = [
                        {"function": {"name": p["name"], "arguments": p["arguments"]},
                         "id": f"call_{turn}_{i}"}
                        for i, p in enumerate(parsed)
                    ]
                    content = ""  # Was actually a tool call, not text

            if content and not tool_calls:
                # Model gave diagnosis — we're done
                result.diagnosis_text = content
                result.turns.append(asdict(TurnLog(
                    turn=turn, role="assistant", text=content[:1000])))
                break

            if tool_calls:
                for tc in tool_calls:
                    func = tc.get("function", {})
                    tool_name = func.get("name", "")
                    tool_args = func.get("arguments", {})
                    if isinstance(tool_args, str):
                        try:
                            tool_args = json.loads(tool_args)
                        except json.JSONDecodeError:
                            tool_args = {"raw": tool_args}

                    result.tools_used.append(tool_name)
                    all_tool_calls.append({"name": tool_name, "args": tool_args})

                    # Execute tool call
                    if container_id:
                        tool_output = execute_tool_call(container_id, tool_name, tool_args)
                        tool_output = add_error_feedback(tool_output, tool_name, tool_args)
                    else:
                        tool_output = "[no container — dry run mode]"

                    result.turns.append(asdict(TurnLog(
                        turn=turn, role="tool",
                        tool_name=tool_name, tool_args=tool_args,
                        tool_output=tool_output[:500])))

                    print(f"    {tool_name}({json.dumps(tool_args)[:80]}) → {tool_output[:100]}...")

                    # Add to conversation
                    messages.append({
                        "role": "assistant",
                        "content": None,
                        "tool_calls": [tc],
                    })
                    messages.append({
                        "role": "tool",
                        "content": tool_output,
                        "tool_call_id": tc.get("id", f"call_{turn}"),
                    })
                # Chain-of-thought guidance between turns (disabled — causes
                # loops with small models that treat guidance as new queries)
                # if turn < max_turns - 1:
                #     guidance = build_turn_guidance(turn, max_turns, result.turns)
                #     messages.append({"role": "user", "content": guidance})

            elif not content:
                # Empty response
                result.error = "Empty model response"
                break

        # If we ran out of turns without diagnosis, take last content
        if not result.diagnosis_text and content:
            result.diagnosis_text = content

        # Score: check expected keywords in diagnosis + tool outputs
        all_text = result.diagnosis_text.lower()
        for turn_log in result.turns:
            if isinstance(turn_log, dict):
                if turn_log.get("tool_output"):
                    all_text += " " + turn_log["tool_output"].lower()
                if turn_log.get("text"):
                    all_text += " " + turn_log["text"].lower()

        for kw in scenario.expected_keywords:
            if kw.lower() in all_text:
                result.keywords_found.append(kw)
            else:
                result.keywords_missing.append(kw)

        # Pass if we found at least half the keywords and used appropriate tools
        keyword_ratio = len(result.keywords_found) / max(len(scenario.expected_keywords), 1)
        tool_match = (
            not scenario.expected_tools or  # knowledge questions
            any(t in result.tools_used for t in scenario.expected_tools)
        )
        result.passed = keyword_ratio >= 0.5 and tool_match

    except Exception as e:
        result.error = str(e)
    finally:
        if container_id:
            teardown_container(container_name)
        result.duration_s = time.time() - start_time

    return result


def main():
    parser = argparse.ArgumentParser(description="Termfix Real-World Evaluation")
    parser.add_argument("--server", default="http://localhost:8098",
                       help="llama-server URL")
    parser.add_argument("--output", default="realworld-eval-results",
                       help="Output directory")
    parser.add_argument("--max-turns", type=int, default=5,
                       help="Max tool-calling turns per scenario")
    parser.add_argument("--scenario", type=str, default=None,
                       help="Run specific scenario by name")
    parser.add_argument("--no-containers", action="store_true",
                       help="Skip container setup (test model responses only)")
    parser.add_argument("--verbose", action="store_true",
                       help="Print full model responses")
    args = parser.parse_args()

    # Check server health
    try:
        resp = requests.get(f"{args.server}/health", timeout=5)
        print(f"Server: {args.server} — {resp.json()}")
    except Exception as e:
        print(f"ERROR: Cannot reach server at {args.server}: {e}")
        sys.exit(1)

    # Select scenarios
    scenarios = SCENARIOS
    if args.scenario:
        scenarios = [s for s in SCENARIOS if s.name == args.scenario]
        if not scenarios:
            print(f"Unknown scenario: {args.scenario}")
            print(f"Available: {', '.join(s.name for s in SCENARIOS)}")
            sys.exit(1)

    print(f"\n=== Termfix Real-World Evaluation ===")
    print(f"Scenarios: {len(scenarios)}")
    print(f"Max turns: {args.max_turns}")
    print(f"Containers: {'yes' if not args.no_containers else 'no'}")
    print()

    # Run scenarios
    results = []
    passed = 0
    failed = 0
    errors = 0

    for i, scenario in enumerate(scenarios):
        print(f"[{i+1}/{len(scenarios)}] {scenario.name} ({scenario.category}, {scenario.difficulty})")
        print(f"  Query: {scenario.description[:80]}...")

        result = run_scenario(
            scenario, args.server,
            max_turns=args.max_turns,
            use_containers=not args.no_containers
        )
        results.append(result)

        if result.error:
            status = f"ERROR: {result.error[:60]}"
            errors += 1
        elif result.passed:
            status = "PASS"
            passed += 1
        else:
            status = f"FAIL (missing: {result.keywords_missing})"
            failed += 1

        print(f"  Result: {status} ({result.duration_s:.1f}s)")
        if args.verbose and result.diagnosis_text:
            print(f"  Diagnosis: {result.diagnosis_text[:200]}...")
        print()

    # Summary
    total = len(results)
    print("=" * 60)
    print(f"  RESULTS: {passed}/{total} passed ({passed/total*100:.0f}%)")
    print(f"  Passed:  {passed}")
    print(f"  Failed:  {failed}")
    print(f"  Errors:  {errors}")
    print()

    # Per-category
    categories = {}
    for r in results:
        cat = r.category
        if cat not in categories:
            categories[cat] = {"passed": 0, "total": 0}
        categories[cat]["total"] += 1
        if r.passed:
            categories[cat]["passed"] += 1

    print("  Per-category:")
    for cat in sorted(categories.keys()):
        c = categories[cat]
        pct = c["passed"] / c["total"] * 100
        print(f"    {cat:15s}: {c['passed']}/{c['total']} ({pct:.0f}%)")

    # Save results
    os.makedirs(args.output, exist_ok=True)
    timestamp = datetime.utcnow().strftime("%Y%m%dT%H%M%SZ")

    with open(os.path.join(args.output, f"results-{timestamp}.json"), "w") as f:
        json.dump({
            "timestamp": timestamp,
            "server": args.server,
            "summary": {
                "total": total, "passed": passed,
                "failed": failed, "errors": errors,
                "pass_rate": passed / total if total else 0,
            },
            "categories": categories,
            "results": [asdict(r) for r in results],
        }, f, indent=2, default=str)

    # Save failures for review
    with open(os.path.join(args.output, f"failures-{timestamp}.jsonl"), "w") as f:
        for r in results:
            if not r.passed:
                f.write(json.dumps(asdict(r), default=str) + "\n")

    print(f"\n  Results saved to {args.output}/")
    print("=" * 60)


if __name__ == "__main__":
    main()
