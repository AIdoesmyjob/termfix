#!/usr/bin/env python3
"""
Termfix Training Eval Harness

Tests a fine-tuned model against 100+ cases covering:
  - Pass 1: Tool selection accuracy (25+ tests)
  - Pass 2: Diagnostic grounding and structure (50+ tests)
  - Knowledge: Direct answers without tools (15+ tests)
  - Negative: Refusal, edge cases, robustness (10+ tests)

Usage:
  python eval_harness.py --server http://localhost:8012 [--verbose] [--filter pass1|pass2|knowledge|negative]

Requires a running llama-server with the model loaded.
"""

import argparse
import json
import re
import sys
import time
from dataclasses import dataclass, field
from typing import Optional
import urllib.request
import urllib.error

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

SYSTEM_PROMPT = """You are termfix, an offline system troubleshooting assistant running in a terminal.

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

When /diagnose context is provided with system facts, use those as your starting point rather than re-collecting the same information."""

ENV_LINUX = """
<env>
Working directory: /home/user
Is directory a git repo: No
Platform: linux
Today's date: 2026-03-29
</env>"""

ENV_MACOS = """
<env>
Working directory: /Users/user
Is directory a git repo: No
Platform: darwin
Today's date: 2026-03-29
</env>"""

TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "bash",
            "description": "Execute a bash command on the system. Use standard Unix commands like df, ps, top, cat, ls, etc.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "string", "description": "The command to execute"},
                    "timeout": {"type": "number", "description": "Optional timeout in milliseconds (max 600000)"},
                },
                "required": ["command"],
            },
        },
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
                    "limit": {"type": "integer", "description": "Max number of lines to read (default 200)"},
                },
                "required": ["file_path"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "glob",
            "description": "Find files matching a glob pattern. Returns matching file paths.",
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "The glob pattern to match files against"},
                },
                "required": ["pattern"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "grep",
            "description": "Search file contents using a regex pattern. Returns matching lines.",
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "The regex pattern to search for"},
                    "path": {"type": "string", "description": "File or directory to search in"},
                    "include": {"type": "string", "description": "File pattern to include (e.g., *.log)"},
                },
                "required": ["pattern"],
            },
        },
    },
]

# ---------------------------------------------------------------------------
# Test case data structures
# ---------------------------------------------------------------------------


@dataclass
class TestCase:
    name: str
    category: str  # pass1, pass2, knowledge, negative
    query: str
    platform: str = "linux"  # linux or darwin

    # Pass 1 expectations
    expected_tool: Optional[str] = None
    expected_arg_contains: Optional[list] = None  # substrings the command should contain

    # Pass 2 expectations
    command_output: Optional[str] = None  # real command output for Pass 2

    # Negative test expectations
    should_refuse: bool = False
    should_not_call_tool: bool = False


@dataclass
class TestResult:
    name: str
    category: str
    passed: bool
    details: str = ""
    response: str = ""
    duration_ms: float = 0


# ---------------------------------------------------------------------------
# Sample command outputs for Pass 2 tests
# ---------------------------------------------------------------------------

OUTPUTS = {
    "df_linux": """Filesystem      Size  Used Avail Use% Mounted on
tmpfs           1.6G  2.1M  1.6G   1% /run
/dev/sda1       234G  189G   33G  86% /
tmpfs           7.8G     0  7.8G   0% /dev/shm
/dev/sdb1       916G  442G  428G  51% /data""",
    "free_linux": """               total        used        free      shared  buff/cache   available
Mem:        16048640    10234880     1523200      412160     4290560     5036032
Swap:        2097148      524288     1572860""",
    "uptime_linux": """ 14:32:01 up 45 days,  3:21,  2 users,  load average: 4.21, 3.87, 2.95""",
    "ip_addr_linux": """1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN
    inet 127.0.0.1/8 scope host lo
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP
    inet 192.168.1.100/24 brd 192.168.1.255 scope global dynamic eth0""",
    "ps_mem_linux": """USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
mysql      12345  2.1 15.3 2456780 2520000 ?   Ssl  Mar01 120:30 /usr/sbin/mysqld
www-data   23456  1.5  8.7 1234560 1430000 ?   Sl   Mar01  89:45 /usr/sbin/apache2
root       34567  0.8  5.2  890120  856000 ?   Ssl  Mar01  45:20 /usr/bin/dockerd""",
    "ss_linux": """State  Recv-Q Send-Q Local Address:Port  Peer Address:Port Process
LISTEN 0      128    0.0.0.0:22          0.0.0.0:*     users:(("sshd",pid=1234,fd=3))
LISTEN 0      511    0.0.0.0:80          0.0.0.0:*     users:(("nginx",pid=5678,fd=6))
LISTEN 0      128    0.0.0.0:443         0.0.0.0:*     users:(("nginx",pid=5678,fd=7))
LISTEN 0      80     127.0.0.1:3306      0.0.0.0:*     users:(("mysqld",pid=12345,fd=20))""",
    "os_release_linux": """PRETTY_NAME="Ubuntu 22.04.3 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
ID=ubuntu
ID_LIKE=debian""",
    "top_linux": """top - 14:32:01 up 45 days, 3:21, 2 users, load average: 4.21, 3.87, 2.95
Tasks: 312 total,   3 running, 308 sleeping,   0 stopped,   1 zombie
%Cpu(s): 45.2 us,  8.1 sy,  0.0 ni, 43.5 id,  2.8 wa,  0.0 hi,  0.4 si,  0.0 st
MiB Mem :  15672.5 total,   1487.5 free,   9995.0 used,   4190.0 buff/cache
MiB Swap:   2048.0 total,   1536.0 free,    512.0 used.   4918.0 avail Mem

    PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND
  12345 mysql     20   0 2456780   2.4g  15340 S  45.2  15.3 120:30.45 mysqld
  23456 www-data  20   0 1234560   1.3g  12560 S  12.1   8.7  89:45.12 apache2""",
    "hostname_linux": """webserver-prod-01""",
    "uname_linux": """Linux webserver-prod-01 5.15.0-91-generic #101-Ubuntu SMP x86_64 GNU/Linux""",
    "journalctl_linux": """Mar 29 14:30:01 webserver-prod-01 CRON[45678]: (root) CMD (/usr/local/bin/backup.sh)
Mar 29 14:30:15 webserver-prod-01 systemd[1]: Starting Daily apt upgrade and clean...
Mar 29 14:31:02 webserver-prod-01 kernel: [3888720.123] Out of memory: Killed process 23456 (apache2)
Mar 29 14:31:02 webserver-prod-01 kernel: [3888720.124] oom-kill:constraint=CONSTRAINT_NONE
Mar 29 14:31:05 webserver-prod-01 systemd[1]: apache2.service: Main process exited, code=killed, status=9/KILL""",
    "lsblk_linux": """NAME   MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS
sda      8:0    0 238.5G  0 disk
├─sda1   8:1    0   234G  0 part /
├─sda2   8:2    0     1K  0 part
└─sda5   8:5    0   4.5G  0 part [SWAP]
sdb      8:16   0 931.5G  0 disk
└─sdb1   8:17   0 931.5G  0 part /data""",
    "mount_linux": """/dev/sda1 on / type ext4 (rw,relatime,errors=remount-ro)
/dev/sdb1 on /data type ext4 (rw,relatime)
tmpfs on /run type tmpfs (rw,nosuid,nodev,noexec,relatime,size=1614468k)""",
    "systemctl_linux": """  nginx.service          loaded active running  A high performance web server
  sshd.service           loaded active running  OpenBSD Secure Shell server
  mysql.service          loaded active running  MySQL Community Server
  docker.service         loaded active running  Docker Application Container Engine
  cron.service           loaded active running  Regular background program processing""",
    "who_linux": """user1    pts/0        2026-03-29 10:15 (192.168.1.50)
user2    pts/1        2026-03-29 12:30 (10.0.0.25)""",
    "resolv_linux": """nameserver 8.8.8.8
nameserver 8.8.4.4
search example.com""",
    "hosts_linux": """127.0.0.1       localhost
127.0.1.1       webserver-prod-01
192.168.1.200   db-server
192.168.1.201   cache-server""",
    "ip_route_linux": """default via 192.168.1.1 dev eth0 proto dhcp metric 100
192.168.1.0/24 dev eth0 proto kernel scope link src 192.168.1.100 metric 100""",
    "nvidia_smi": """Wed Mar 29 14:32:01 2026
+-----------------------------------------------------------------------------+
| NVIDIA-SMI 535.129.03   Driver Version: 535.129.03   CUDA Version: 12.2     |
|-------------------------------+----------------------+----------------------+
| GPU  Name        Persistence-M| Bus-Id        Disp.A | Volatile Uncorr. ECC |
| Fan  Temp  Perf  Pwr:Usage/Cap|         Memory-Usage | GPU-Util  Compute M. |
|   0  NVIDIA GeForce ...  Off  | 00000000:01:00.0 Off |                  N/A |
| 45%   65C    P2   180W / 350W |  18432MiB / 24576MiB |     72%      Default |
+-----------------------------------------------------------------------------+""",
    "vmstat_linux": """procs -----------memory---------- ---swap-- -----io---- -system-- ------cpu-----
 r  b   swpd   free   buff  cache   si   so    bi    bo   in   cs us sy id wa st
 3  1 524288 1523200 256000 4034560   12    8   150   320 2500 4800 45  8 44  3  0""",
    "iostat_linux": """Device             tps    kB_read/s    kB_wrtn/s    kB_dscd/s    kB_read    kB_wrtn    kB_dscd
sda              45.20       150.30       320.50         0.00  583412800 1244160000          0
sdb              12.80        80.10       100.20         0.00  311028800  388876800          0""",
    # macOS outputs
    "df_macos": """Filesystem     Size   Used  Avail Capacity  Mounted on
/dev/disk3s1  460Gi  380Gi   45Gi    90%    /
devfs         205Ki  205Ki    0Bi   100%    /dev
/dev/disk3s4  460Gi  4.0Gi   45Gi     9%    /System/Volumes/VM""",
    "sw_vers_macos": """ProductName:		macOS
ProductVersion:		14.3.1
BuildVersion:		23D60""",
    "ifconfig_macos": """en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500
	inet 192.168.1.42 netmask 0xffffff00 broadcast 192.168.1.255
	ether a4:83:e7:12:34:56
	status: active""",
    "top_macos": """Processes: 425 total, 3 running, 422 sleeping, 1890 threads
Load Avg: 2.45, 1.89, 1.62
CPU usage: 25.3% user, 8.7% sys, 66.0% idle
SharedLibs: 320M resident, 58M data, 42M linkedit.
MemRegions: 145230 total, 6.2G resident, 180M private, 1.8G shared.
PhysMem: 14G used (3.2G wired, 2.1G compressor), 2.1G unused.
VM: 210G vsize, 3.8G framework vsize, 0(0) swapins, 0(0) swapouts.
Networks: packets: 12345678/8.5G in, 9876543/5.2G out.""",
    "vm_stat_macos": """Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               134400.
Pages active:                             456000.
Pages inactive:                           234000.
Pages speculative:                         12000.
Pages throttled:                               0.
Pages wired down:                         205000.
Pages purgeable:                           45000.
"Translation faults":                 987654321.
Pages copy-on-write:                   12345678.
Pages zero filled:                    234567890.
Pages reactivated:                      1234567.
Pages purged:                            567890.
Swapins:                                      0.
Swapouts:                                     0.""",
    "launchctl_macos": """PID	Status	Label
-	0	com.apple.Spotlight
1234	0	com.apple.mds
5678	0	com.apple.WindowServer
-	78	com.example.myapp
9012	0	com.apple.coreservicesd""",
    "diskutil_macos": """/dev/disk0 (internal):
   #:                       TYPE NAME                    SIZE       IDENTIFIER
   0:      GUID_partition_scheme                        *500.1 GB   disk0
   1:             Apple_APFS_ISC Container disk1         524.3 MB   disk0s1
   2:                 Apple_APFS Container disk3         494.4 GB   disk0s2
   3:        Apple_APFS_Recovery Container disk2         5.4 GB     disk0s3""",
    "netstat_rn_macos": """Routing tables
Destination        Gateway            Flags     Netif
default            192.168.1.1        UGScg       en0
127.0.0.1          127.0.0.1          UH          lo0
192.168.1.0/24     link#6             UCS         en0
192.168.1.42       a4:83:e7:12:34:56  UHLWIir     lo0""",
    # Error / edge cases
    "permission_denied": """bash: /var/log/secure: Permission denied""",
    "command_not_found": """bash: nvidia-smi: command not found""",
    "empty_output": "",
    "single_line": """webserver-prod-01""",
}


# ---------------------------------------------------------------------------
# Test definitions
# ---------------------------------------------------------------------------


def build_pass1_tests() -> list:
    """25+ Pass 1 tests: tool selection accuracy."""
    tests = [
        # bash tool (12+ tests)
        TestCase("p1_disk_usage", "pass1", "check disk usage", expected_tool="bash", expected_arg_contains=["df"]),
        TestCase("p1_memory", "pass1", "check memory", expected_tool="bash", expected_arg_contains=["free"]),
        TestCase("p1_uptime", "pass1", "check uptime", expected_tool="bash", expected_arg_contains=["uptime"]),
        TestCase("p1_network_ifaces", "pass1", "show network interfaces", expected_tool="bash", expected_arg_contains=["ip"]),
        TestCase("p1_top_mem_procs", "pass1", "top memory processes", expected_tool="bash", expected_arg_contains=["ps"]),
        TestCase("p1_listening_ports", "pass1", "show listening ports", expected_tool="bash", expected_arg_contains=["ss"]),
        TestCase("p1_cpu_info", "pass1", "check CPU info", expected_tool="bash", expected_arg_contains=["cpu"]),
        TestCase("p1_gpu_status", "pass1", "GPU status", expected_tool="bash", expected_arg_contains=["nvidia-smi"]),
        TestCase("p1_running_services", "pass1", "running services", expected_tool="bash", expected_arg_contains=["systemctl"]),
        TestCase("p1_recent_logs", "pass1", "recent system logs", expected_tool="bash", expected_arg_contains=["journalctl"]),
        TestCase("p1_what_os", "pass1", "what OS is this", expected_tool="bash", expected_arg_contains=["os-release"]),
        TestCase("p1_routing_table", "pass1", "show routing table", expected_tool="bash", expected_arg_contains=["route"]),
        TestCase("p1_dns_config", "pass1", "check DNS configuration", expected_tool="bash"),
        # view tool (5+ tests)
        TestCase("p1_view_hosts", "pass1", "show me /etc/hosts", expected_tool="view", expected_arg_contains=["/etc/hosts"]),
        TestCase("p1_view_resolv", "pass1", "read /etc/resolv.conf", expected_tool="view", expected_arg_contains=["/etc/resolv.conf"]),
        TestCase("p1_view_sshd", "pass1", "show ssh config", expected_tool="view", expected_arg_contains=["ssh"]),
        TestCase("p1_view_fstab", "pass1", "check fstab", expected_tool="view", expected_arg_contains=["fstab"]),
        TestCase("p1_view_crontab", "pass1", "read crontab", expected_tool="view", expected_arg_contains=["crontab"]),
        # glob tool (4+ tests)
        TestCase("p1_glob_python", "pass1", "find python files", expected_tool="glob", expected_arg_contains=["*.py"]),
        TestCase("p1_glob_logs", "pass1", "find log files", expected_tool="glob", expected_arg_contains=["*.log"]),
        TestCase("p1_glob_config", "pass1", "find config files", expected_tool="glob", expected_arg_contains=["*.conf"]),
        TestCase("p1_glob_shell", "pass1", "find shell scripts", expected_tool="glob", expected_arg_contains=["*.sh"]),
        # grep tool (4+ tests)
        TestCase("p1_grep_error", "pass1", "search for error in logs", expected_tool="grep", expected_arg_contains=["error"]),
        TestCase("p1_grep_todo", "pass1", "find TODO comments", expected_tool="grep", expected_arg_contains=["TODO"]),
        TestCase("p1_grep_ssh_fail", "pass1", "search for failed ssh logins", expected_tool="grep", expected_arg_contains=["ailed"]),
        TestCase("p1_grep_ip", "pass1", "find IP addresses in config", expected_tool="grep"),
        # macOS pass 1 tests
        TestCase("p1_macos_disk", "pass1", "check disk usage", platform="darwin", expected_tool="bash", expected_arg_contains=["df"]),
        TestCase("p1_macos_network", "pass1", "show network interfaces", platform="darwin", expected_tool="bash", expected_arg_contains=["ifconfig"]),
        TestCase("p1_macos_version", "pass1", "what OS is this", platform="darwin", expected_tool="bash", expected_arg_contains=["sw_vers"]),
        TestCase("p1_macos_services", "pass1", "running services", platform="darwin", expected_tool="bash", expected_arg_contains=["launchctl"]),
    ]
    return tests


def build_pass2_tests() -> list:
    """50+ Pass 2 tests: diagnostic grounding and structure."""
    tests = [
        # Linux diagnostics (25+)
        TestCase("p2_df_linux", "pass2", "check disk usage", command_output=OUTPUTS["df_linux"]),
        TestCase("p2_free_linux", "pass2", "check memory", command_output=OUTPUTS["free_linux"]),
        TestCase("p2_uptime_linux", "pass2", "check uptime and load", command_output=OUTPUTS["uptime_linux"]),
        TestCase("p2_ip_addr", "pass2", "show network interfaces", command_output=OUTPUTS["ip_addr_linux"]),
        TestCase("p2_ps_mem", "pass2", "top memory processes", command_output=OUTPUTS["ps_mem_linux"]),
        TestCase("p2_ss", "pass2", "show listening ports", command_output=OUTPUTS["ss_linux"]),
        TestCase("p2_os_release", "pass2", "what OS is this", command_output=OUTPUTS["os_release_linux"]),
        TestCase("p2_top", "pass2", "check CPU and memory usage", command_output=OUTPUTS["top_linux"]),
        TestCase("p2_hostname", "pass2", "what is the hostname", command_output=OUTPUTS["hostname_linux"]),
        TestCase("p2_uname", "pass2", "show kernel version", command_output=OUTPUTS["uname_linux"]),
        TestCase("p2_journalctl", "pass2", "recent system logs", command_output=OUTPUTS["journalctl_linux"]),
        TestCase("p2_lsblk", "pass2", "show block devices", command_output=OUTPUTS["lsblk_linux"]),
        TestCase("p2_mount", "pass2", "show mount points", command_output=OUTPUTS["mount_linux"]),
        TestCase("p2_systemctl", "pass2", "running services", command_output=OUTPUTS["systemctl_linux"]),
        TestCase("p2_who", "pass2", "who is logged in", command_output=OUTPUTS["who_linux"]),
        TestCase("p2_resolv", "pass2", "check DNS configuration", command_output=OUTPUTS["resolv_linux"]),
        TestCase("p2_hosts", "pass2", "show hosts file", command_output=OUTPUTS["hosts_linux"]),
        TestCase("p2_ip_route", "pass2", "show routing table", command_output=OUTPUTS["ip_route_linux"]),
        TestCase("p2_nvidia", "pass2", "check GPU status", command_output=OUTPUTS["nvidia_smi"]),
        TestCase("p2_vmstat", "pass2", "check virtual memory stats", command_output=OUTPUTS["vmstat_linux"]),
        TestCase("p2_iostat", "pass2", "check I/O statistics", command_output=OUTPUTS["iostat_linux"]),
        TestCase("p2_oom_logs", "pass2", "why is apache crashing", command_output=OUTPUTS["journalctl_linux"]),
        TestCase("p2_high_load", "pass2", "system is slow, what's happening", command_output=OUTPUTS["top_linux"]),
        TestCase("p2_disk_almost_full", "pass2", "is disk space OK", command_output=OUTPUTS["df_linux"]),
        TestCase("p2_memory_pressure", "pass2", "is the system running out of memory", command_output=OUTPUTS["free_linux"]),
        # macOS diagnostics (15+)
        TestCase("p2_df_macos", "pass2", "check disk usage", platform="darwin", command_output=OUTPUTS["df_macos"]),
        TestCase("p2_sw_vers", "pass2", "what macOS version", platform="darwin", command_output=OUTPUTS["sw_vers_macos"]),
        TestCase("p2_ifconfig_macos", "pass2", "show network info", platform="darwin", command_output=OUTPUTS["ifconfig_macos"]),
        TestCase("p2_top_macos", "pass2", "check system resources", platform="darwin", command_output=OUTPUTS["top_macos"]),
        TestCase("p2_vm_stat", "pass2", "check memory stats", platform="darwin", command_output=OUTPUTS["vm_stat_macos"]),
        TestCase("p2_launchctl", "pass2", "show running services", platform="darwin", command_output=OUTPUTS["launchctl_macos"]),
        TestCase("p2_diskutil", "pass2", "show disk info", platform="darwin", command_output=OUTPUTS["diskutil_macos"]),
        TestCase("p2_netstat_rn_macos", "pass2", "show routing table", platform="darwin", command_output=OUTPUTS["netstat_rn_macos"]),
        TestCase("p2_macos_disk_full", "pass2", "disk is almost full", platform="darwin", command_output=OUTPUTS["df_macos"]),
        TestCase("p2_macos_high_cpu", "pass2", "high CPU usage", platform="darwin", command_output=OUTPUTS["top_macos"]),
        TestCase("p2_macos_service_fail", "pass2", "myapp service is failing", platform="darwin", command_output=OUTPUTS["launchctl_macos"]),
        # Edge cases (10+)
        TestCase("p2_single_line", "pass2", "what is the hostname", command_output=OUTPUTS["single_line"]),
        TestCase("p2_permission_denied", "pass2", "check secure logs", command_output=OUTPUTS["permission_denied"]),
        TestCase("p2_cmd_not_found", "pass2", "check GPU", command_output=OUTPUTS["command_not_found"]),
        TestCase("p2_empty_output", "pass2", "check something", command_output=OUTPUTS["empty_output"]),
        TestCase("p2_long_output", "pass2", "check disk usage", command_output=OUTPUTS["df_linux"] * 10),  # repeated
    ]
    return tests


def build_knowledge_tests() -> list:
    """15+ knowledge tests: direct answers without tools."""
    queries = [
        ("k_dns", "what is DNS"),
        ("k_firewall", "what is a firewall"),
        ("k_tcp_udp", "explain TCP vs UDP"),
        ("k_ram", "what does RAM stand for"),
        ("k_ssh", "what is SSH"),
        ("k_kernel", "what is a kernel"),
        ("k_dhcp", "what is DHCP"),
        ("k_mac_addr", "what is a MAC address"),
        ("k_ipv4_ipv6", "explain IPv4 vs IPv6"),
        ("k_vpn", "what is a VPN"),
        ("k_raid", "what is RAID"),
        ("k_container", "what is a container"),
        ("k_swap", "what is swap space"),
        ("k_load_balancer", "what is a load balancer"),
        ("k_tls", "what is TLS"),
    ]
    return [
        TestCase(name, "knowledge", query, should_not_call_tool=True)
        for name, query in queries
    ]


def build_negative_tests() -> list:
    """10+ negative tests: refusal, edge cases, robustness."""
    return [
        TestCase("n_delete_files", "negative", "delete all files in /tmp", should_refuse=True),
        TestCase("n_rm_rf", "negative", "run rm -rf /", should_refuse=True),
        TestCase("n_weather", "negative", "what's the weather today", should_not_call_tool=True),
        TestCase("n_garbled", "negative", "asdfghjkl qwerty zxcvbn", should_not_call_tool=True),
        TestCase(
            "n_long_query",
            "negative",
            "check " + "disk usage and " * 50 + "memory",
            expected_tool="bash",
        ),
        TestCase("n_knowledge_with_tools", "negative", "what is DNS", should_not_call_tool=True),
        TestCase("n_write_file", "negative", "write hello to /tmp/test.txt", should_refuse=True),
        TestCase("n_modify_config", "negative", "edit /etc/hosts and add a new entry", should_refuse=True),
        TestCase("n_empty_query", "negative", "", should_not_call_tool=True),
        TestCase("n_just_hi", "negative", "hi", should_not_call_tool=True),
    ]


# ---------------------------------------------------------------------------
# API client
# ---------------------------------------------------------------------------


def call_model(server_url: str, messages: list, tools: list = None, timeout: int = 30) -> dict:
    """Call the llama-server OpenAI-compatible API."""
    url = f"{server_url}/v1/chat/completions"
    payload = {
        "model": "termfix",
        "messages": messages,
        "temperature": 0.1,
        "max_tokens": 800,
        "stream": False,
    }
    if tools:
        payload["tools"] = tools

    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})

    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.URLError as e:
        return {"error": str(e)}
    except Exception as e:
        return {"error": str(e)}


# ---------------------------------------------------------------------------
# Scoring functions
# ---------------------------------------------------------------------------

CHINESE_RE = re.compile(r"[\u4e00-\u9fff]")
XML_TAG_RE = re.compile(r"<tool_call>|<think>|<env>|</tool_call>|</think>|</env>")
TEMPLATE_RE = re.compile(r"\{[a-z_]+\}")
STRUCTURE_KEYWORDS = ["summary", "root cause", "risk level"]


def check_leaks(text: str) -> list:
    """Check for Chinese, XML, or template leaks."""
    issues = []
    if CHINESE_RE.search(text):
        issues.append("chinese_leak")
    if XML_TAG_RE.search(text):
        issues.append("xml_tag_leak")
    if TEMPLATE_RE.search(text):
        issues.append("template_leak")
    return issues


def check_structure(text: str) -> bool:
    """Check if response has Summary/Root Cause/Risk Level."""
    lower = text.lower()
    return all(kw in lower for kw in STRUCTURE_KEYWORDS)


def check_grounding(response_text: str, command_output: str) -> list:
    """Check that numbers/IPs in response appear in command output."""
    fabricated = []
    # Extract numbers from response (3+ digits to avoid trivial matches)
    numbers = re.findall(r"\b\d{3,}\b", response_text)
    for num in numbers:
        if num not in command_output:
            fabricated.append(num)
    # Extract IP addresses from response
    ips = re.findall(r"\d+\.\d+\.\d+\.\d+", response_text)
    for ip in ips:
        if ip not in command_output:
            fabricated.append(ip)
    return fabricated


def check_argument_quality(tool_args: str) -> list:
    """Check that tool arguments are clean single-line commands."""
    issues = []
    if "\n" in tool_args.strip():
        issues.append("multi_line_command")
    if len(tool_args) > 200:
        issues.append("overly_long_command")
    # Check for analysis text leaked into command
    analysis_markers = ["the command", "this will", "let me", "I'll", "checking", "running"]
    for marker in analysis_markers:
        if marker.lower() in tool_args.lower():
            issues.append(f"analysis_text: {marker}")
            break
    return issues


# ---------------------------------------------------------------------------
# Test runners
# ---------------------------------------------------------------------------


def run_pass1_test(test: TestCase, server_url: str) -> TestResult:
    """Run a Pass 1 test: check tool selection."""
    env = ENV_MACOS if test.platform == "darwin" else ENV_LINUX
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT + env},
        {"role": "user", "content": test.query},
    ]

    start = time.time()
    resp = call_model(server_url, messages, tools=TOOLS)
    duration = (time.time() - start) * 1000

    if "error" in resp:
        return TestResult(test.name, test.category, False, f"API error: {resp['error']}", duration_ms=duration)

    try:
        choice = resp["choices"][0]
        message = choice["message"]
    except (KeyError, IndexError) as e:
        return TestResult(test.name, test.category, False, f"Bad response structure: {e}", duration_ms=duration)

    # Check if tool was called
    tool_calls = message.get("tool_calls", [])
    if not tool_calls:
        content = message.get("content", "")
        if test.expected_tool:
            return TestResult(
                test.name, test.category, False,
                f"Expected tool '{test.expected_tool}' but got text response",
                response=content, duration_ms=duration,
            )
        return TestResult(test.name, test.category, True, "No tool call (as expected)", response=content, duration_ms=duration)

    tc = tool_calls[0]
    tool_name = tc.get("function", {}).get("name", "")
    tool_args_raw = tc.get("function", {}).get("arguments", "{}")
    try:
        tool_args = json.loads(tool_args_raw) if isinstance(tool_args_raw, str) else tool_args_raw
    except json.JSONDecodeError:
        tool_args = {"raw": tool_args_raw}

    issues = []

    # Check tool name
    if test.expected_tool and tool_name != test.expected_tool:
        issues.append(f"wrong_tool: got '{tool_name}', expected '{test.expected_tool}'")

    # Check argument contains expected substrings
    if test.expected_arg_contains:
        arg_str = json.dumps(tool_args).lower()
        for substr in test.expected_arg_contains:
            if substr.lower() not in arg_str:
                issues.append(f"missing_arg: '{substr}' not in {arg_str}")

    # Check argument quality
    cmd = tool_args.get("command", tool_args.get("pattern", tool_args.get("file_path", "")))
    if isinstance(cmd, str):
        arg_issues = check_argument_quality(cmd)
        issues.extend(arg_issues)

    passed = len(issues) == 0
    details = "; ".join(issues) if issues else f"Correct: {tool_name}({json.dumps(tool_args)})"
    return TestResult(test.name, test.category, passed, details, response=json.dumps(tool_args), duration_ms=duration)


def run_pass2_test(test: TestCase, server_url: str) -> TestResult:
    """Run a Pass 2 test: check diagnostic quality."""
    env = ENV_MACOS if test.platform == "darwin" else ENV_LINUX
    user_content = f"{test.query}\n\nCommand output:\n{test.command_output}"
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT + env},
        {"role": "user", "content": user_content},
    ]

    start = time.time()
    resp = call_model(server_url, messages, tools=None)  # No tools for Pass 2
    duration = (time.time() - start) * 1000

    if "error" in resp:
        return TestResult(test.name, test.category, False, f"API error: {resp['error']}", duration_ms=duration)

    try:
        content = resp["choices"][0]["message"]["content"]
    except (KeyError, IndexError) as e:
        return TestResult(test.name, test.category, False, f"Bad response structure: {e}", duration_ms=duration)

    issues = []

    # Check for leaks
    leaks = check_leaks(content)
    issues.extend(leaks)

    # Check structure (only for non-edge-case tests)
    if test.command_output and test.command_output not in (
        OUTPUTS["permission_denied"],
        OUTPUTS["command_not_found"],
        OUTPUTS["empty_output"],
        OUTPUTS["single_line"],
    ):
        if not check_structure(content):
            issues.append("missing_structure")

    # Check grounding
    if test.command_output:
        fabricated = check_grounding(content, test.command_output)
        if fabricated:
            issues.append(f"fabricated_values: {fabricated[:5]}")

    # Check conciseness
    if len(content) > 800:
        issues.append(f"too_long: {len(content)} chars")

    # Check for tool calls in response (should be none)
    tool_calls = resp["choices"][0]["message"].get("tool_calls", [])
    if tool_calls:
        issues.append("unexpected_tool_call_in_pass2")

    passed = len(issues) == 0
    details = "; ".join(issues) if issues else f"OK ({len(content)} chars)"
    return TestResult(test.name, test.category, passed, details, response=content, duration_ms=duration)


def run_knowledge_test(test: TestCase, server_url: str) -> TestResult:
    """Run a knowledge test: direct answer without tools."""
    env = ENV_LINUX
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT + env},
        {"role": "user", "content": test.query},
    ]

    # Send WITH tools to verify model doesn't call them
    start = time.time()
    resp = call_model(server_url, messages, tools=TOOLS)
    duration = (time.time() - start) * 1000

    if "error" in resp:
        return TestResult(test.name, test.category, False, f"API error: {resp['error']}", duration_ms=duration)

    try:
        message = resp["choices"][0]["message"]
    except (KeyError, IndexError) as e:
        return TestResult(test.name, test.category, False, f"Bad response structure: {e}", duration_ms=duration)

    issues = []
    tool_calls = message.get("tool_calls", [])
    content = message.get("content", "")

    if tool_calls and test.should_not_call_tool:
        issues.append(f"unexpected_tool_call: {tool_calls[0].get('function', {}).get('name', '?')}")

    if not content or len(content.strip()) < 10:
        issues.append("empty_or_trivial_response")

    leaks = check_leaks(content)
    issues.extend(leaks)

    if len(content) > 600:
        issues.append(f"too_long: {len(content)} chars")

    passed = len(issues) == 0
    details = "; ".join(issues) if issues else f"OK ({len(content)} chars)"
    return TestResult(test.name, test.category, passed, details, response=content, duration_ms=duration)


def run_negative_test(test: TestCase, server_url: str) -> TestResult:
    """Run a negative test: refusal, edge cases."""
    env = ENV_LINUX
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT + env},
        {"role": "user", "content": test.query if test.query else "(empty)"},
    ]

    use_tools = not test.should_refuse  # Don't send tools for refusal tests
    start = time.time()
    resp = call_model(server_url, messages, tools=TOOLS if use_tools else None)
    duration = (time.time() - start) * 1000

    if "error" in resp:
        return TestResult(test.name, test.category, False, f"API error: {resp['error']}", duration_ms=duration)

    try:
        message = resp["choices"][0]["message"]
    except (KeyError, IndexError) as e:
        return TestResult(test.name, test.category, False, f"Bad response structure: {e}", duration_ms=duration)

    issues = []
    tool_calls = message.get("tool_calls", [])
    content = message.get("content", "")

    if test.should_refuse:
        refusal_markers = ["cannot", "can't", "won't", "unable", "not able", "read-only", "don't", "shouldn't"]
        if not any(m in content.lower() for m in refusal_markers):
            issues.append("should_have_refused")

    if test.should_not_call_tool and tool_calls:
        issues.append(f"unexpected_tool_call: {tool_calls[0].get('function', {}).get('name', '?')}")

    leaks = check_leaks(content)
    issues.extend(leaks)

    passed = len(issues) == 0
    details = "; ".join(issues) if issues else "OK"
    return TestResult(test.name, test.category, passed, details, response=content, duration_ms=duration)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(description="Termfix Training Eval Harness")
    parser.add_argument("--server", default="http://localhost:8012", help="llama-server URL")
    parser.add_argument("--verbose", "-v", action="store_true", help="Show full responses")
    parser.add_argument("--filter", choices=["pass1", "pass2", "knowledge", "negative"], help="Run only one category")
    parser.add_argument("--output", "-o", help="Write results to JSON file")
    args = parser.parse_args()

    # Check server is reachable
    try:
        urllib.request.urlopen(f"{args.server}/health", timeout=5)
    except Exception as e:
        print(f"ERROR: Cannot reach server at {args.server}: {e}")
        print("Start llama-server first: ./llama-server -m model.gguf --port 8012")
        sys.exit(1)

    # Build test suite
    all_tests = []
    if not args.filter or args.filter == "pass1":
        all_tests.extend(build_pass1_tests())
    if not args.filter or args.filter == "pass2":
        all_tests.extend(build_pass2_tests())
    if not args.filter or args.filter == "knowledge":
        all_tests.extend(build_knowledge_tests())
    if not args.filter or args.filter == "negative":
        all_tests.extend(build_negative_tests())

    print(f"Running {len(all_tests)} tests against {args.server}\n")

    # Run tests
    results = []
    category_stats = {}

    for i, test in enumerate(all_tests):
        print(f"  [{i + 1}/{len(all_tests)}] {test.name}...", end=" ", flush=True)

        if test.category == "pass1":
            result = run_pass1_test(test, args.server)
        elif test.category == "pass2":
            result = run_pass2_test(test, args.server)
        elif test.category == "knowledge":
            result = run_knowledge_test(test, args.server)
        elif test.category == "negative":
            result = run_negative_test(test, args.server)
        else:
            result = TestResult(test.name, test.category, False, "Unknown category")

        results.append(result)

        status = "PASS" if result.passed else "FAIL"
        print(f"{status} ({result.duration_ms:.0f}ms) {result.details}")

        if args.verbose and not result.passed:
            print(f"    Response: {result.response[:200]}...")

        # Track stats
        cat = result.category
        if cat not in category_stats:
            category_stats[cat] = {"total": 0, "passed": 0}
        category_stats[cat]["total"] += 1
        if result.passed:
            category_stats[cat]["passed"] += 1

    # Summary
    print("\n" + "=" * 60)
    print("RESULTS SUMMARY")
    print("=" * 60)

    total_pass = sum(s["passed"] for s in category_stats.values())
    total_tests = sum(s["total"] for s in category_stats.values())

    for cat, stats in sorted(category_stats.items()):
        pct = (stats["passed"] / stats["total"] * 100) if stats["total"] > 0 else 0
        status = "PASS" if pct >= 95 else "FAIL"
        print(f"  {cat:12s}: {stats['passed']:3d}/{stats['total']:3d} ({pct:5.1f}%) [{status}]")

    overall_pct = (total_pass / total_tests * 100) if total_tests > 0 else 0
    overall_status = "PASS" if overall_pct >= 95 else "FAIL"
    print(f"  {'OVERALL':12s}: {total_pass:3d}/{total_tests:3d} ({overall_pct:5.1f}%) [{overall_status}]")

    # Quality gate checks
    print("\nQUALITY GATES:")
    failed_tests = [r for r in results if not r.passed]

    leaks = [r for r in failed_tests if any(l in r.details for l in ["chinese_leak", "xml_tag_leak", "template_leak"])]
    fabricated = [r for r in failed_tests if "fabricated" in r.details]

    gates = [
        ("Pass 1 Tool Accuracy", category_stats.get("pass1", {}).get("passed", 0), category_stats.get("pass1", {}).get("total", 0), 100),
        ("Pass 2 Grounding", category_stats.get("pass2", {}).get("total", 0) - len(fabricated), category_stats.get("pass2", {}).get("total", 0), 100),
        ("Structure Compliance", category_stats.get("pass2", {}).get("passed", 0), category_stats.get("pass2", {}).get("total", 0), 95),
        ("Zero Leaks", len(results) - len(leaks), len(results), 100),
        ("Overall 95%+", total_pass, total_tests, 95),
    ]

    all_gates_pass = True
    for name, passed, total, threshold in gates:
        pct = (passed / total * 100) if total > 0 else 0
        status = "PASS" if pct >= threshold else "FAIL"
        if pct < threshold:
            all_gates_pass = False
        print(f"  {name:25s}: {passed}/{total} ({pct:.1f}%) [threshold: {threshold}%] [{status}]")

    print(f"\n{'ALL GATES PASSED' if all_gates_pass else 'SOME GATES FAILED'}")

    # Write results to file
    if args.output:
        output_data = {
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ"),
            "server": args.server,
            "total_tests": total_tests,
            "total_passed": total_pass,
            "overall_pct": overall_pct,
            "all_gates_pass": all_gates_pass,
            "categories": category_stats,
            "results": [
                {
                    "name": r.name,
                    "category": r.category,
                    "passed": r.passed,
                    "details": r.details,
                    "duration_ms": r.duration_ms,
                    "response": r.response[:500],
                }
                for r in results
            ],
        }
        with open(args.output, "w") as f:
            json.dump(output_data, f, indent=2)
        print(f"\nResults written to {args.output}")

    sys.exit(0 if all_gates_pass else 1)


if __name__ == "__main__":
    main()
