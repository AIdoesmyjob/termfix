package agent

import (
	"strings"
	"testing"

	"github.com/opencode-ai/opencode/internal/diagnose"
	"github.com/opencode-ai/opencode/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"df", `{"command":"df -h"}`, "df -h"},
		{"free", `{"command":"free -m"}`, "free -m"},
		{"empty", `{}`, ""},
		{"invalid json", `not json`, ""},
		{"no command key", `{"cmd":"ls"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractCommand(tt.input))
		})
	}
}

func TestDetectCommandKey(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"df -h", "df"},
		{"free -m", "free"},
		{"uptime", "uptime"},
		{"top -bn1", "top"},
		{"ps aux", "ps"},
		{"ip addr", "ip"},
		{"ifconfig", "ifconfig"},
		{"ss -tulnp", "ss"},
		{"lsof -i", "lsof"},
		{"uname -a", "uname"},
		{"sw_vers", "sw_vers"},
		{"vm_stat", "vm_stat"},
		{"sysctl -a", "sysctl"},
		{"cat /etc/hosts", "cat_etc"},
		{"cat /etc/resolv.conf", "cat_etc"},
		{"/usr/bin/df -h", "df"},
		{"/usr/sbin/ifconfig", "ifconfig"},
		{"cat /tmp/foo", ""}, // not /etc/
		{"docker ps", ""},
		{"ls -la", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			assert.Equal(t, tt.want, detectCommandKey(tt.command))
		})
	}
}

func TestTryStructuredDiagnostic_Fallback(t *testing.T) {
	// Unknown command should fall through
	toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"docker ps"}`}}
	toolResults := []message.ToolResult{{Content: "CONTAINER ID  IMAGE\n123  nginx"}}
	result, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.False(t, ok)
	assert.Empty(t, result)
}

func TestTryStructuredDiagnostic_NonBash(t *testing.T) {
	// Non-bash tool should fall through
	toolCalls := []message.ToolCall{{Name: "grep", Input: `{"pattern":"error"}`}}
	toolResults := []message.ToolResult{{Content: "some output"}}
	_, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.False(t, ok)
}

func TestTryStructuredDiagnostic_Error(t *testing.T) {
	// Error results should fall through to model
	toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"df -h"}`}}
	toolResults := []message.ToolResult{{Content: "Error: command not found", IsError: true}}
	_, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.False(t, ok)
}

func TestTryStructuredDiagnostic_ErrorPrefix(t *testing.T) {
	// Error content prefix should fall through
	toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"df -h"}`}}
	toolResults := []message.ToolResult{{Content: "Error: permission denied"}}
	_, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.False(t, ok)
}

func TestTryStructuredDiagnostic_EmptyOutput(t *testing.T) {
	toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"df -h"}`}}
	toolResults := []message.ToolResult{{Content: ""}}
	_, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.False(t, ok)
}

func TestTryStructuredDiagnostic_MultipleToolCalls(t *testing.T) {
	// Multiple tool calls should fall through
	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"df -h"}`},
		{Name: "bash", Input: `{"command":"free -m"}`},
	}
	toolResults := []message.ToolResult{{Content: "some output"}}
	_, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.False(t, ok)
}

func TestTryStructuredRecipeDiagnostic_ServiceFailure(t *testing.T) {
	recipe := diagnose.SelectRecipe("nginx won't start")
	require.NotNil(t, recipe)

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"systemctl status nginx --no-pager --full -n 20"}`},
		{Name: "bash", Input: `{"command":"journalctl -u nginx -n 40 --no-pager"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "Loaded: loaded (/usr/lib/systemd/system/nginx.service)\nActive: failed (Result: exit-code)\nMain PID: 1234 (code=exited, status=1/FAILURE)"},
		{Content: "Apr 01 nginx[1234]: bind() to 0.0.0.0:80 failed (98: Address already in use)\nApr 01 systemd[1]: nginx.service: Failed with result 'exit-code'."},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "nginx is failing to start due to port conflict")
	assert.Contains(t, result, "Address already in use")
}

func TestTryStructuredRecipeDiagnostic_DNSResolution(t *testing.T) {
	recipe := diagnose.SelectRecipe("dns resolution is broken")
	require.NotNil(t, recipe)

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"cat /etc/resolv.conf"}`},
		{Name: "bash", Input: `{"command":"ip route"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "nameserver 1.1.1.1\nsearch corp.local"},
		{Content: "default via 192.168.1.1 dev eth0"},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "DNS and routing look present")
	assert.Contains(t, result, "Nameservers")
	assert.Contains(t, result, "Default route")
}

// --- Disk Usage ---

const dfLinuxOutput = `Filesystem      Size  Used Avail Use% Mounted on
udev            7.8G     0  7.8G   0% /dev
tmpfs           1.6G  2.1M  1.6G   1% /run
/dev/sda1       458G  376G   59G  87% /
tmpfs           7.8G   84M  7.7G   2% /dev/shm
/dev/sdb1       916G  183G  687G  22% /data`

func TestParseDiskUsage_Linux(t *testing.T) {
	result, ok := parseDiskUsage(dfLinuxOutput)
	require.True(t, ok)

	assert.Contains(t, result, "**Summary**: Disk usage: highest partition at 87%")
	assert.Contains(t, result, "**Risk Level**: Medium")
	assert.Contains(t, result, "/: 376G used of 458G (87%), 59G available")
	assert.Contains(t, result, "/data: 183G used of 916G (22%), 687G available")
	assert.Contains(t, result, "Remediation")
}

const dfMacOutput = `Filesystem       Size   Used  Avail Capacity iused      ifree %iused  Mounted on
/dev/disk3s1s1  460Gi   14Gi  226Gi     6%  501032 2369478968    0%   /
devfs           213Ki  213Ki    0Bi   100%     738          0  100%   /dev
/dev/disk3s6    460Gi  5.0Gi  226Gi     3%       5 2369980000    0%   /System/Volumes/VM
/dev/disk3s2    460Gi  378Mi  226Gi     1%    1077 2369979923    0%   /System/Volumes/Preboot
/dev/disk3s4    460Gi  3.4Gi  226Gi     2%     420 2369980580    0%   /System/Volumes/Update
/dev/disk1s2    500Mi  6.0Mi  482Mi     2%       1    4933608    0%   /System/Volumes/xarts
/dev/disk3s5    460Gi  217Gi  226Gi    49% 2078450 2367901550    0%   /System/Volumes/Data`

func TestParseDiskUsage_Mac(t *testing.T) {
	result, ok := parseDiskUsage(dfMacOutput)
	require.True(t, ok)

	// devfs shows 100% — that should be the highest
	assert.Contains(t, result, "highest partition at 100%")
	assert.Contains(t, result, "**Risk Level**: Critical")
	assert.Contains(t, result, "/System/Volumes/Data")
}

func TestParseDiskUsage_CriticalUsage(t *testing.T) {
	input := `Filesystem  Size  Used Avail Use% Mounted on
/dev/sda1   100G   98G    2G  98% /`
	result, ok := parseDiskUsage(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Critical")
	assert.Contains(t, result, "98%")
}

func TestParseDiskUsage_Empty(t *testing.T) {
	_, ok := parseDiskUsage("Filesystem  Size  Used Avail Use% Mounted on\n")
	assert.False(t, ok)
}

// --- Memory ---

const freeHumanOutput = `               total        used        free      shared  buff/cache   available
Mem:            15Gi       8.2Gi       3.1Gi       1.2Gi       4.0Gi       6.5Gi
Swap:          2.0Gi       0.1Gi       1.9Gi`

func TestParseMemory_Human(t *testing.T) {
	result, ok := parseMemory(freeHumanOutput)
	require.True(t, ok)

	assert.Contains(t, result, "**Summary**: Memory usage at")
	assert.Contains(t, result, "RAM: 8.2Gi used of 15Gi")
	assert.Contains(t, result, "6.5Gi available")
	assert.Contains(t, result, "Swap: 0.1Gi used of 2.0Gi")
}

const freeMBOutput = `              total        used        free      shared  buff/cache   available
Mem:          16384        8400        3200        1200        4100        6600
Swap:          2048         100        1948`

func TestParseMemory_MB(t *testing.T) {
	result, ok := parseMemory(freeMBOutput)
	require.True(t, ok)

	assert.Contains(t, result, "Memory usage at")
	assert.Contains(t, result, "RAM: 8400 used of 16384")
}

func TestParseMemory_HighUsage(t *testing.T) {
	input := `              total        used        free      shared  buff/cache   available
Mem:          16384       15000         200         100        1184         384
Swap:          2048        1800         248`
	result, ok := parseMemory(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Critical") // 91% >= 90 threshold
}

func TestParseMemory_Empty(t *testing.T) {
	_, ok := parseMemory("")
	assert.False(t, ok)
}

// --- Uptime ---

func TestParseUptime_Linux(t *testing.T) {
	input := " 14:23:05 up 42 days,  3:15,  2 users,  load average: 1.23, 0.98, 0.76"
	result, ok := parseUptime(input)
	require.True(t, ok)

	assert.Contains(t, result, "42 days")
	assert.Contains(t, result, "1.23 (1m)")
	assert.Contains(t, result, "**Risk Level**: Low")
}

func TestParseUptime_Mac(t *testing.T) {
	input := "14:23  up 42 days,  3:15, 2 users, load averages: 5.23 3.98 2.76"
	result, ok := parseUptime(input)
	require.True(t, ok)

	assert.Contains(t, result, "5.23 (1m)")
	assert.Contains(t, result, "**Risk Level**: High")
}

func TestParseUptime_CriticalLoad(t *testing.T) {
	input := " 10:00:00 up 1 day,  2:00,  1 user,  load average: 12.50, 10.20, 8.75"
	result, ok := parseUptime(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Critical")
}

func TestParseUptime_Empty(t *testing.T) {
	_, ok := parseUptime("")
	assert.False(t, ok)
}

// --- Top ---

const topOutput = `top - 14:23:05 up 42 days,  3:15,  2 users,  load average: 1.23, 0.98, 0.76
Tasks: 312 total,   1 running, 311 sleeping,   0 stopped,   0 zombie
%Cpu(s):  5.3 us,  2.1 sy,  0.0 ni, 92.1 id,  0.1 wa,  0.0 hi,  0.4 si,  0.0 st
MiB Mem :  15919.3 total,   3204.5 free,   8432.1 used,   4282.7 buff/cache
MiB Swap:   2048.0 total,   1948.0 free,    100.0 used.   6612.8 avail Mem

    PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND
   1234 user      20   0 3456789  12345   6789 S  45.2   3.4   1234:56 firefox
   5678 root      20   0  234567   8901   4567 S  12.3   2.1    567:89 Xorg`

func TestParseTop(t *testing.T) {
	result, ok := parseTop(topOutput)
	require.True(t, ok)

	assert.Contains(t, result, "CPU usage at 7.9%")
	assert.Contains(t, result, "312 total, 1 running")
	assert.Contains(t, result, "8432.1 MiB used of 15919.3 MiB total")
	assert.Contains(t, result, "**Risk Level**: Low")
}

func TestParseTop_HighCPU(t *testing.T) {
	input := `top - 10:00:00 up 1 day
Tasks: 100 total,   5 running
%Cpu(s): 90.0 us,  5.0 sy,  0.0 ni,  3.0 id,  2.0 wa,  0.0 hi,  0.0 si,  0.0 st
MiB Mem :   8192.0 total,    512.0 free,   7000.0 used,    680.0 buff/cache

    PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND
   1000 root      20   0  100000  50000  10000 R  95.0  60.0   100:00 stress`
	result, ok := parseTop(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Critical")
}

// --- Processes ---

const psOutput = `USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
root         1  0.0  0.0 169328 13156 ?        Ss   Mar05   0:09 /sbin/init
user      1234 45.2  3.4 3456789 12345 ?       Sl   Mar06 1234:56 /usr/bin/firefox
root      5678  0.1  0.5 234567  8901 ?        Ss   Mar05  567:89 /usr/lib/xorg/Xorg
nobody    9999  0.0  0.1  12345  2345 ?        S    Mar10   0:01 /usr/sbin/dnsmasq`

func TestParseProcesses(t *testing.T) {
	result, ok := parseProcesses(psOutput)
	require.True(t, ok)

	assert.Contains(t, result, "4 processes running")
	assert.Contains(t, result, "highest CPU: 45.2%")
	assert.Contains(t, result, "PID 1234 (user): CPU 45.2%")
	assert.Contains(t, result, "**Risk Level**: Low") // 45.2% CPU < 50% threshold
}

// --- Network (ip addr) ---

const ipAddrOutput = `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host
       valid_lft forever preferred_lft forever
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP group default qlen 1000
    link/ether aa:bb:cc:dd:ee:ff brd ff:ff:ff:ff:ff:ff
    inet 192.168.1.100/24 brd 192.168.1.255 scope global dynamic eth0
       valid_lft 86400sec preferred_lft 86400sec
3: wlan0: <BROADCAST,MULTICAST> mtu 1500 qdisc noop state DOWN group default qlen 1000
    link/ether 11:22:33:44:55:66 brd ff:ff:ff:ff:ff:ff`

func TestParseNetwork(t *testing.T) {
	result, ok := parseNetwork(ipAddrOutput)
	require.True(t, ok)

	assert.Contains(t, result, "3 network interfaces")
	assert.Contains(t, result, "**Risk Level**: Medium") // wlan0 is DOWN
	assert.Contains(t, result, "lo: UNKNOWN")
	assert.Contains(t, result, "eth0: UP")
	assert.Contains(t, result, "192.168.1.100/24")
	assert.Contains(t, result, "wlan0: DOWN")
}

func TestParseNetwork_AllUp(t *testing.T) {
	input := `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    inet 127.0.0.1/8 scope host lo
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP group default qlen 1000
    inet 10.0.0.5/24 scope global eth0`
	result, ok := parseNetwork(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Low")
}

// --- Ifconfig ---

const ifconfigOutput = `eth0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1500
        inet 192.168.1.100  netmask 255.255.255.0  broadcast 192.168.1.255
        ether aa:bb:cc:dd:ee:ff  txqueuelen 1000  (Ethernet)
        RX packets 123456  bytes 678901234 (647.3 MiB)
        RX errors 0  dropped 0  overruns 0  frame 0
        TX packets 654321  bytes 987654321 (941.9 MiB)
        TX errors 0  dropped 0 overruns 0  carrier 0  collisions 0

lo: flags=73<UP,LOOPBACK,RUNNING>  mtu 65536
        inet 127.0.0.1  netmask 255.0.0.0
        loop  txqueuelen 1000  (Local Loopback)
        RX packets 1000  bytes 50000 (48.8 KiB)
        RX errors 0  dropped 0  overruns 0  frame 0
        TX packets 1000  bytes 50000 (48.8 KiB)
        TX errors 0  dropped 0 overruns 0  carrier 0  collisions 0`

func TestParseIfconfig(t *testing.T) {
	result, ok := parseIfconfig(ifconfigOutput)
	require.True(t, ok)

	assert.Contains(t, result, "2 network interfaces")
	assert.Contains(t, result, "0 total errors")
	assert.Contains(t, result, "eth0: 192.168.1.100")
	assert.Contains(t, result, "lo: 127.0.0.1")
	assert.Contains(t, result, "**Risk Level**: Low")
}

func TestParseIfconfig_WithErrors(t *testing.T) {
	input := `eth0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1500
        inet 10.0.0.1  netmask 255.255.255.0
        RX errors 15  dropped 0  overruns 0  frame 0
        TX errors 3  dropped 0 overruns 0  carrier 0  collisions 0`
	result, ok := parseIfconfig(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Medium")
	assert.Contains(t, result, "18 errors")
}

// --- Sockets ---

const ssOutput = `State  Recv-Q Send-Q  Local Address:Port   Peer Address:Port Process
LISTEN 0      128     0.0.0.0:22           0.0.0.0:*        users:(("sshd",pid=1234,fd=3))
LISTEN 0      511     0.0.0.0:80           0.0.0.0:*        users:(("nginx",pid=5678,fd=6))
LISTEN 0      128     127.0.0.1:5432       0.0.0.0:*        users:(("postgres",pid=9012,fd=5))`

func TestParseSockets(t *testing.T) {
	result, ok := parseSockets(ssOutput)
	require.True(t, ok)

	assert.Contains(t, result, "3 listening sockets")
	assert.Contains(t, result, "**Risk Level**: Medium") // port 22 and 80 on 0.0.0.0
	assert.Contains(t, result, "0.0.0.0:22")
	assert.Contains(t, result, "0.0.0.0:80")
	assert.Contains(t, result, "binding to 127.0.0.1")
}

func TestParseSockets_LocalOnly(t *testing.T) {
	input := `State  Recv-Q Send-Q  Local Address:Port   Peer Address:Port Process
LISTEN 0      128     127.0.0.1:5432       0.0.0.0:*        users:(("postgres",pid=100,fd=5))`
	result, ok := parseSockets(input)
	require.True(t, ok)
	assert.Contains(t, result, "**Risk Level**: Low")
}

// --- Lsof ---

const lsofOutput = `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
sshd     1234 root    3u  IPv4  12345      0t0  TCP *:22 (LISTEN)
nginx    5678 root    6u  IPv4  67890      0t0  TCP *:80 (LISTEN)
firefox  9012 user   50u  IPv4  11111      0t0  TCP 192.168.1.100:54321->93.184.216.34:443 (ESTABLISHED)`

func TestParseLsof(t *testing.T) {
	result, ok := parseLsof(lsofOutput)
	require.True(t, ok)

	assert.Contains(t, result, "2 listening, 1 established")
	assert.Contains(t, result, "sshd (PID 1234")
	assert.Contains(t, result, "firefox (PID 9012")
}

// --- Uname ---

func TestParseUname_Linux(t *testing.T) {
	input := "Linux hostname 5.15.0-91-generic #101-Ubuntu SMP Tue Nov 14 13:30:08 UTC 2023 x86_64 x86_64 x86_64 GNU/Linux"
	result, ok := parseUname(input)
	require.True(t, ok)

	assert.Contains(t, result, "Linux")
	assert.Contains(t, result, "hostname")
	assert.Contains(t, result, "5.15.0-91-generic")
	assert.Contains(t, result, "**Risk Level**: Low")
}

func TestParseUname_Mac(t *testing.T) {
	input := "Darwin MacBook-Pro.local 23.2.0 Darwin Kernel Version 23.2.0 x86_64"
	result, ok := parseUname(input)
	require.True(t, ok)

	assert.Contains(t, result, "Darwin")
	assert.Contains(t, result, "23.2.0")
}

// --- sw_vers ---

func TestParseSwVers(t *testing.T) {
	input := `ProductName:		macOS
ProductVersion:		14.2.1
BuildVersion:		23C71`
	result, ok := parseSwVers(input)
	require.True(t, ok)

	assert.Contains(t, result, "macOS")
	assert.Contains(t, result, "14.2.1")
	assert.Contains(t, result, "23C71")
	assert.Contains(t, result, "**Risk Level**: Low")
}

func TestParseSwVers_Empty(t *testing.T) {
	_, ok := parseSwVers("")
	assert.False(t, ok)
}

// --- vm_stat ---

const vmStatOutput = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               12345.
Pages active:                             67890.
Pages inactive:                           23456.
Pages speculative:                         7890.
Pages throttled:                              0.
Pages wired down:                         45678.
Pages purgeable:                           1234.
"Translation faults":                 123456789.
Pages copy-on-write:                   12345678.
Pages zero filled:                     98765432.
Pages reactivated:                       567890.
Pages purged:                            123456.
File-backed pages:                        34567.
Anonymous pages:                          56789.
Pages stored in compressor:               78901.
Pages occupied by compressor:             12345.
Decompressions:                          234567.
Compressions:                            345678.
Pageins:                                 456789.
Pageouts:                                  1234.
Swapins:                                      0.
Swapouts:                                     0.`

func TestParseVmStat(t *testing.T) {
	result, ok := parseVmStat(vmStatOutput)
	require.True(t, ok)

	assert.Contains(t, result, "Page size: 16384 bytes")
	// 12345 free out of ~161714 total = ~7.6%
	assert.Contains(t, result, "Free:")
	assert.Contains(t, result, "Active:")
	assert.Contains(t, result, "Wired:")
	assert.Contains(t, result, "pages free")
}

func TestParseVmStat_LowFree(t *testing.T) {
	input := `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                                 500.
Pages active:                            500000.
Pages inactive:                           10000.
Pages wired down:                        200000.
Pages occupied by compressor:             50000.`
	result, ok := parseVmStat(input)
	require.True(t, ok)
	// 500 free out of 760500 = ~0.07% — Critical
	assert.Contains(t, result, "**Risk Level**: Critical")
}

// --- Sysctl ---

func TestParseSysctl(t *testing.T) {
	input := `kern.ostype = Darwin
kern.osrelease = 23.2.0
kern.hostname = MacBook-Pro.local
hw.ncpu = 8
hw.memsize = 17179869184`
	result, ok := parseSysctl(input)
	require.True(t, ok)

	assert.Contains(t, result, "5 entries")
	assert.Contains(t, result, "kern.ostype = Darwin")
	assert.Contains(t, result, "**Risk Level**: Low")
}

// --- /etc file ---

func TestParseEtcFile(t *testing.T) {
	input := `# This is /etc/hosts
# Comments here
127.0.0.1	localhost
::1		localhost
10.0.0.1	gateway`
	result, ok := parseEtcFile("cat /etc/hosts", input)
	require.True(t, ok)

	assert.Contains(t, result, "/etc/hosts")
	assert.Contains(t, result, "5 lines, 3 non-comment")
	assert.Contains(t, result, "127.0.0.1")
	assert.Contains(t, result, "gateway")
	// Comments should be filtered from findings
	assert.NotContains(t, result, "# This is")
}

// --- Render ---

func TestDiagnosticResult_Render(t *testing.T) {
	d := DiagnosticResult{
		Summary:     "Test summary",
		RiskLevel:   "High",
		Findings:    []string{"Finding 1", "Finding 2"},
		Remediation: []string{"Fix 1", "Fix 2"},
	}
	result := d.Render()

	assert.True(t, strings.HasPrefix(result, "**Summary**: Test summary\n"))
	assert.Contains(t, result, "**Risk Level**: High\n")
	assert.Contains(t, result, "- Finding 1\n")
	assert.Contains(t, result, "- Finding 2\n")
	assert.Contains(t, result, "1. Fix 1\n")
	assert.Contains(t, result, "2. Fix 2\n")
}

func TestDiagnosticResult_RenderNoRemediation(t *testing.T) {
	d := DiagnosticResult{
		Summary:   "All good",
		RiskLevel: "Low",
		Findings:  []string{"Everything nominal"},
	}
	result := d.Render()

	assert.Contains(t, result, "**Summary**: All good")
	assert.NotContains(t, result, "**Remediation**")
}

// --- parseSize ---

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"15Gi", 15 * 1024},
		{"8.2Gi", 8.2 * 1024},
		{"2.0Gi", 2.0 * 1024},
		{"16384", 16384}, // plain number = MB
		{"512Mi", 512},
		{"1.5G", 1.5 * 1024},
		{"100K", 100.0 / 1024},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSize(tt.input)
			assert.InDelta(t, tt.want, got, 0.01, "parseSize(%q)", tt.input)
		})
	}
}

// --- Integration-style test: full roundtrip ---

func TestTryStructuredDiagnostic_DfRoundtrip(t *testing.T) {
	toolCalls := []message.ToolCall{{
		Name:  "bash",
		Input: `{"command":"df -h"}`,
	}}
	toolResults := []message.ToolResult{{
		Content: dfLinuxOutput,
	}}
	result, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "**Summary**")
	assert.Contains(t, result, "87%")
	assert.NotContains(t, result, "[?]") // No fabricated-value markers
}

func TestTryStructuredDiagnostic_FreeRoundtrip(t *testing.T) {
	toolCalls := []message.ToolCall{{
		Name:  "bash",
		Input: `{"command":"free -h"}`,
	}}
	toolResults := []message.ToolResult{{
		Content: freeHumanOutput,
	}}
	result, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "**Summary**")
	assert.Contains(t, result, "RAM:")
}
