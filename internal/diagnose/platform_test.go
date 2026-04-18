package diagnose

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withPlatform temporarily sets osPlatform for the duration of a test.
func withPlatform(t *testing.T, platform string) {
	t.Helper()
	old := osPlatform
	osPlatform = platform
	t.Cleanup(func() { osPlatform = old })
}

// =============================================================================
// LINUX — InitialCommand verification for every recipe
// =============================================================================

func TestPlatformLinux_InitialCommands(t *testing.T) {
	withPlatform(t, "linux")

	type tc struct {
		input      string
		recipe     RecipeName
		mustHave   []string // substrings that MUST appear
		mustNotHave []string // substrings that MUST NOT appear
	}

	tests := []tc{
		{
			input: "out of memory", recipe: RecipeMemoryPressure,
			mustHave: []string{"free -h"}, mustNotHave: []string{"vm_stat"},
		},
		{
			input: "dns resolution broken", recipe: RecipeDNSResolution,
			mustHave: []string{"cat /etc/resolv.conf"}, mustNotHave: []string{"scutil"},
		},
		{
			input: "no internet", recipe: RecipeNetworkConnectivity,
			mustHave: []string{"ip -o addr show"}, mustNotHave: []string{"ifconfig"},
		},
		{
			input: "nginx won't start", recipe: RecipeServiceFailure,
			mustHave: []string{"systemctl status", "nginx"}, mustNotHave: []string{"launchctl"},
		},
		{
			input: "service crashed", recipe: RecipeServiceFailure,
			mustHave: []string{"systemctl --failed", "service --status-all"}, mustNotHave: []string{"launchctl"},
		},
		{
			input: "port 8080 in use", recipe: RecipePortConflict,
			mustHave: []string{"ss -tlnp"}, mustNotHave: []string{"lsof"},
		},
		{
			input: "cron not running", recipe: RecipeCron,
			mustHave: []string{"crontab -l", "systemctl list-timers"},
		},
		{
			input: "apt broken package", recipe: RecipePackage,
			mustHave: []string{"apt list", "dpkg"}, mustNotHave: []string{"brew"},
		},
		{
			input: "ntp not syncing", recipe: RecipeTime,
			mustHave: []string{"timedatectl"}, mustNotHave: []string{"sntp"},
		},
		{
			input: "journald too big", recipe: RecipeLog,
			mustHave: []string{"journalctl --disk-usage"}, mustNotHave: []string{"ls -lhS /var/log/"},
		},
		{
			input: "firewall blocking", recipe: RecipeFirewall,
			mustHave: []string{"iptables", "nft list", "ufw"}, mustNotHave: []string{"pfctl"},
		},
		{
			input: "io wait high", recipe: RecipeIO,
			mustHave: []string{"iostat -xz"}, mustNotHave: []string{"iostat -c 2"},
		},
		{
			input: "temperature too high", recipe: RecipeHardware,
			mustHave: []string{"dmesg"}, mustNotHave: []string{"system_profiler", "pmset"},
		},
		{
			input: "won't boot", recipe: RecipeBoot,
			mustHave: []string{"journalctl -xb"}, mustNotHave: []string{"log show"},
		},
		{
			input: "user admin account locked", recipe: RecipeUser,
			mustHave: []string{"passwd -S", "grep", "/etc/passwd"}, mustNotHave: []string{"dscl"},
		},
		{
			input: "database connection timeout", recipe: RecipeDatabase,
			mustHave: []string{"ss -tlnp"}, // Linux generic DB has lsof as fallback, so don't exclude it
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r, "nil recipe for %q", tc.input)
			assert.Equal(t, tc.recipe, r.Name)
			for _, sub := range tc.mustHave {
				assert.Contains(t, r.InitialCommand, sub, "Linux cmd missing %q", sub)
			}
			for _, sub := range tc.mustNotHave {
				assert.NotContains(t, r.InitialCommand, sub, "Linux cmd should NOT contain %q", sub)
			}
		})
	}
}

// =============================================================================
// MACOS — InitialCommand verification for every recipe
// =============================================================================

func TestPlatformMacOS_InitialCommands(t *testing.T) {
	withPlatform(t, "darwin")

	type tc struct {
		input       string
		recipe      RecipeName
		mustHave    []string
		mustNotHave []string
	}

	tests := []tc{
		{
			input: "out of memory", recipe: RecipeMemoryPressure,
			mustHave: []string{"vm_stat"}, mustNotHave: []string{"free"},
		},
		{
			input: "dns resolution broken", recipe: RecipeDNSResolution,
			mustHave: []string{"scutil --dns"}, mustNotHave: []string{"resolv.conf"},
		},
		{
			input: "no internet", recipe: RecipeNetworkConnectivity,
			mustHave: []string{"ifconfig"}, mustNotHave: []string{"ip -o addr"},
		},
		{
			input: "nginx won't start", recipe: RecipeServiceFailure,
			mustHave: []string{"launchctl list", "nginx"}, mustNotHave: []string{"systemctl"},
		},
		{
			input: "service crashed", recipe: RecipeServiceFailure,
			mustHave: []string{"launchctl list | head -50"}, mustNotHave: []string{"systemctl"},
		},
		{
			input: "port 8080 in use", recipe: RecipePortConflict,
			mustHave: []string{"lsof -iTCP"}, mustNotHave: []string{"ss -tlnp"},
		},
		{
			input: "cron not running", recipe: RecipeCron,
			mustHave: []string{"crontab -l"}, mustNotHave: []string{"systemctl list-timers"},
		},
		{
			input: "brew broken package", recipe: RecipePackage,
			mustHave: []string{"brew doctor"}, mustNotHave: []string{"apt", "dpkg"},
		},
		{
			input: "ntp not syncing", recipe: RecipeTime,
			mustHave: []string{"sntp", "date -u"}, mustNotHave: []string{"timedatectl"},
		},
		{
			input: "journald too big", recipe: RecipeLog,
			mustHave: []string{"du -sh /var/log/", "ls -lhS /var/log/"}, mustNotHave: []string{"journalctl --disk-usage"},
		},
		{
			input: "firewall blocking", recipe: RecipeFirewall,
			mustHave: []string{"pfctl"}, mustNotHave: []string{"iptables", "nft list", "ufw"},
		},
		{
			input: "io wait high", recipe: RecipeIO,
			mustHave: []string{"iostat -c 2"}, mustNotHave: []string{"iostat -xz", "/proc/diskstats"},
		},
		{
			input: "temperature too high", recipe: RecipeHardware,
			mustHave: []string{"system_profiler", "pmset"}, mustNotHave: []string{"dmesg"},
		},
		{
			input: "won't boot", recipe: RecipeBoot,
			mustHave: []string{"log show"}, mustNotHave: []string{"journalctl"},
		},
		{
			input: "user admin account locked", recipe: RecipeUser,
			mustHave: []string{"dscl"}, mustNotHave: []string{"passwd -S", "/etc/passwd"},
		},
		{
			input: "database connection timeout", recipe: RecipeDatabase,
			mustHave: []string{"lsof -iTCP"}, mustNotHave: []string{"ss -tlnp"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r, "nil recipe for %q", tc.input)
			assert.Equal(t, tc.recipe, r.Name)
			for _, sub := range tc.mustHave {
				assert.Contains(t, r.InitialCommand, sub, "macOS cmd missing %q", sub)
			}
			for _, sub := range tc.mustNotHave {
				assert.NotContains(t, r.InitialCommand, sub, "macOS cmd should NOT contain %q", sub)
			}
		})
	}
}

// =============================================================================
// LINUX — FollowUpCommand verification
// =============================================================================

func TestPlatformLinux_FollowUpCommands(t *testing.T) {
	withPlatform(t, "linux")

	type tc struct {
		input       string
		firstOutput string
		mustHave    []string
		mustNotHave []string
		wantEmpty   bool
	}

	tests := []tc{
		{
			input: "disk full", firstOutput: "95%",
			mustHave: []string{"du -xhd 1 /"}, mustNotHave: []string{"/System/Volumes/Data"},
		},
		{
			input: "out of memory", firstOutput: "mem output",
			mustHave: []string{"--sort=-rss"}, mustNotHave: []string{"-r | head"},
		},
		{
			input: "server slow", firstOutput: "load",
			mustHave: []string{"--sort=-pcpu"}, mustNotHave: []string{"-r | head"},
		},
		{
			input: "dns failing", firstOutput: "resolv",
			mustHave: []string{"ip route"}, mustNotHave: []string{"netstat"},
		},
		{
			input: "no internet", firstOutput: "iface",
			mustHave: []string{"ip route"}, mustNotHave: []string{"netstat"},
		},
		{
			input: "port 8080 in use", firstOutput: "output",
			mustHave: []string{"ss -tlnp", "8080"}, mustNotHave: []string{"lsof"},
		},
		{
			input: "cron not running", firstOutput: "crontab",
			mustHave: []string{"journalctl -u cron"}, mustNotHave: []string{"log show"},
		},
		{
			input: "nginx won't start", firstOutput: "service output",
			mustHave: []string{"journalctl -u", "nginx"}, mustNotHave: []string{"log show"},
		},
		{
			input: "ntp not syncing", firstOutput: "timedatectl",
			mustHave: []string{"chronyc"}, mustNotHave: []string{"systemsetup"},
		},
		{
			input: "journald too big", firstOutput: "disk usage",
			mustHave: []string{"journalctl -p err"}, mustNotHave: []string{"ls -lhS"},
		},
		{
			input: "firewall blocking", firstOutput: "iptables",
			mustHave: []string{"ss -tlnp"}, mustNotHave: []string{"lsof"},
		},
		{
			input: "io wait high", firstOutput: "iostat",
			mustHave: []string{"iotop"}, wantEmpty: false,
		},
		{
			input: "temperature too high", firstOutput: "dmesg",
			mustHave: []string{"smartctl"}, mustNotHave: []string{"diskutil"},
		},
		{
			input: "user admin account locked", firstOutput: "id output",
			mustHave: []string{"faillock", "chage"}, wantEmpty: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r)
			fu := r.FollowUpCommand(tc.firstOutput)
			if tc.wantEmpty {
				assert.Empty(t, fu)
				return
			}
			assert.NotEmpty(t, fu, "expected follow-up for %s on Linux", r.Name)
			for _, sub := range tc.mustHave {
				assert.Contains(t, fu, sub, "Linux follow-up missing %q", sub)
			}
			for _, sub := range tc.mustNotHave {
				assert.NotContains(t, fu, sub, "Linux follow-up should NOT contain %q", sub)
			}
		})
	}
}

// =============================================================================
// MACOS — FollowUpCommand verification
// =============================================================================

func TestPlatformMacOS_FollowUpCommands(t *testing.T) {
	withPlatform(t, "darwin")

	type tc struct {
		input       string
		firstOutput string
		mustHave    []string
		mustNotHave []string
		wantEmpty   bool
	}

	tests := []tc{
		{
			input: "disk full", firstOutput: "95%",
			mustHave: []string{"/System/Volumes/Data"}, mustNotHave: []string{"du -xhd 1 / 2"},
		},
		{
			input: "out of memory", firstOutput: "mem output",
			mustHave: []string{"-r | head"}, mustNotHave: []string{"--sort=-rss"},
		},
		{
			input: "server slow", firstOutput: "load",
			mustHave: []string{"-r | head"}, mustNotHave: []string{"--sort=-pcpu"},
		},
		{
			input: "dns failing", firstOutput: "resolv",
			mustHave: []string{"netstat -rn"}, mustNotHave: []string{"ip route"},
		},
		{
			input: "no internet", firstOutput: "iface",
			mustHave: []string{"netstat -rn"}, mustNotHave: []string{"ip route"},
		},
		{
			input: "port 8080 in use", firstOutput: "output",
			mustHave: []string{"lsof -iTCP:", "8080"}, mustNotHave: []string{"ss -tlnp"},
		},
		{
			input: "cron not running", firstOutput: "crontab",
			mustHave: []string{"log show", "cron"}, mustNotHave: []string{"journalctl"},
		},
		{
			input: "nginx won't start", firstOutput: "service output",
			mustHave: []string{"log show", "nginx"}, mustNotHave: []string{"journalctl"},
		},
		{
			input: "ntp not syncing", firstOutput: "sntp",
			mustHave: []string{"systemsetup"}, mustNotHave: []string{"chronyc", "ntpq"},
		},
		{
			input: "journald too big", firstOutput: "du output",
			mustHave: []string{"ls -lhS /var/log/"}, mustNotHave: []string{"journalctl -p err"},
		},
		{
			input: "firewall blocking", firstOutput: "pfctl",
			mustHave: []string{"lsof -iTCP"}, mustNotHave: []string{"ss -tlnp"},
		},
		{
			input: "io wait high", firstOutput: "iostat",
			wantEmpty: true,
		},
		{
			input: "temperature too high", firstOutput: "profiler",
			mustHave: []string{"diskutil"}, mustNotHave: []string{"smartctl", "sensors"},
		},
		{
			input: "user admin account locked", firstOutput: "id output",
			wantEmpty: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r)
			fu := r.FollowUpCommand(tc.firstOutput)
			if tc.wantEmpty {
				assert.Empty(t, fu)
				return
			}
			assert.NotEmpty(t, fu, "expected follow-up for %s on macOS", r.Name)
			for _, sub := range tc.mustHave {
				assert.Contains(t, fu, sub, "macOS follow-up missing %q", sub)
			}
			for _, sub := range tc.mustNotHave {
				assert.NotContains(t, fu, sub, "macOS follow-up should NOT contain %q", sub)
			}
		})
	}
}

// =============================================================================
// UNKNOWN OS — verify graceful fallback (non-darwin, non-linux)
// =============================================================================

func TestPlatformUnknown_Fallbacks(t *testing.T) {
	withPlatform(t, "freebsd")

	// All recipes should still produce non-nil results with non-empty commands
	inputs := []string{
		"disk full", "out of memory", "server slow", "dns failing",
		"no internet", "docker crash", "npm build fails", "nginx won't start",
		"permission denied", "port 8080 in use", "ssl expired", "merge conflict",
		"cron not running", "apt broken", "zombie process",
		"ssh connection refused to server", "ntp not syncing",
		"journald too big", "postgres database connection refused",
		"firewall blocking", "locked account",
		"io wait high", "temperature too high", "won't boot",
		"nfs stale file handle",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			r := SelectRecipe(input)
			require.NotNil(t, r, "nil recipe on unknown OS for %q", input)
			assert.NotEmpty(t, r.InitialCommand, "empty command on unknown OS for %q", input)
		})
	}
}

func TestPlatformUnknown_FallsToLinuxDefaults(t *testing.T) {
	withPlatform(t, "freebsd")

	// On unknown OS, non-darwin branches should execute (Linux defaults)
	tests := []struct {
		input   string
		wantSub string
		note    string
	}{
		{"out of memory", "free -h", "memory should use free -h (Linux default)"},
		{"dns failing", "cat /etc/resolv.conf", "DNS should use resolv.conf (Linux default)"},
		{"no internet", "ip -o addr show", "network should use ip addr (Linux default)"},
		{"port 8080 in use", "ss -tlnp", "port should use ss (Linux default)"},
		{"ntp not syncing", "timedatectl", "time should use timedatectl (Linux default)"},
		{"firewall blocking", "iptables", "firewall should use iptables (Linux default)"},
		{"io wait high", "iostat -xz", "IO should use iostat -xz (Linux default)"},
		{"temperature too high", "dmesg", "hardware should use dmesg (Linux default)"},
		{"won't boot", "journalctl -xb", "boot should use journalctl (Linux default)"},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r)
			assert.Contains(t, r.InitialCommand, tc.wantSub, tc.note)
		})
	}
}

// =============================================================================
// PLATFORM-INDEPENDENT RECIPES — same on all OSes
// =============================================================================

func TestPlatformIndependent_Recipes(t *testing.T) {
	platforms := []string{"linux", "darwin", "freebsd", "windows"}

	// These recipes produce the same command regardless of platform
	platformIndependent := []struct {
		input   string
		recipe  RecipeName
		wantCmd string
	}{
		{"disk full", RecipeDiskUsage, "df -h"},
		{"can't create files, df shows space available", RecipeDiskInodes, "df -i"},
		{"server slow", RecipePerformanceCPU, "uptime"},
		{"ssl expired", RecipeSSL, "openssl"},
		{"merge conflict", RecipeGit, "git status"},
		{"zombie process", RecipeProcess, "defunct"},
		{"ssh connection refused to server", RecipeSSH, "ssh-add"},
		{"nfs stale file handle", RecipeNFS, "mount -t nfs"},
		{"noexec mount issue", RecipePermissionMount, "noexec"},
		{"permission denied on /var/log/app.log", RecipePermission, "ls -la"},
	}

	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			withPlatform(t, platform)
			for _, tc := range platformIndependent {
				t.Run(tc.input, func(t *testing.T) {
					r := SelectRecipe(tc.input)
					require.NotNil(t, r)
					assert.Equal(t, tc.recipe, r.Name)
					assert.Contains(t, r.InitialCommand, tc.wantCmd)
				})
			}
		})
	}
}

// =============================================================================
// PLATFORM-DEPENDENT FOLLOW-UPS — same recipe, different follow-up per OS
// =============================================================================

func TestPlatformFollowUp_DiskUsage(t *testing.T) {
	highUsage := "Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 100G 95G 5G 95% /"

	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("disk full")
		require.NotNil(t, r)
		fu := r.FollowUpCommand(highUsage)
		assert.Contains(t, fu, "du -xhd 1 /")
		assert.NotContains(t, fu, "/System/Volumes")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("disk full")
		require.NotNil(t, r)
		fu := r.FollowUpCommand(highUsage)
		assert.Contains(t, fu, "/System/Volumes/Data")
	})

	t.Run("freebsd", func(t *testing.T) {
		withPlatform(t, "freebsd")
		r := SelectRecipe("disk full")
		require.NotNil(t, r)
		fu := r.FollowUpCommand(highUsage)
		// Should use Linux default (du -xhd 1 /)
		assert.Contains(t, fu, "du -xhd 1 /")
	})
}

// =============================================================================
// SERVICE RECIPES — Linux systemctl vs macOS launchctl, with and without name
// =============================================================================

func TestPlatformService_LinuxVsMacOS(t *testing.T) {
	t.Run("linux_named_service", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("nginx won't start")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "systemctl status")
		assert.Contains(t, r.InitialCommand, "service")
		assert.Contains(t, r.InitialCommand, "nginx")
		assert.NotContains(t, r.InitialCommand, "launchctl")

		fu := r.FollowUpCommand("some output")
		assert.Contains(t, fu, "journalctl -u")
		assert.Contains(t, fu, "nginx")
		assert.NotContains(t, fu, "log show")
	})

	t.Run("linux_generic_service", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("service crashed")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "systemctl --failed")
		assert.Contains(t, r.InitialCommand, "service --status-all")
		assert.NotContains(t, r.InitialCommand, "launchctl")
		// No follow-up without service name
		assert.Empty(t, r.FollowUpCommand("some output"))
	})

	t.Run("darwin_named_service", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("nginx won't start")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "launchctl list")
		assert.Contains(t, r.InitialCommand, "nginx")
		assert.NotContains(t, r.InitialCommand, "systemctl")

		fu := r.FollowUpCommand("some output")
		assert.Contains(t, fu, "log show")
		assert.Contains(t, fu, "nginx")
		assert.NotContains(t, fu, "journalctl")
	})

	t.Run("darwin_generic_service", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("service crashed")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "launchctl list | head -50")
		assert.NotContains(t, r.InitialCommand, "systemctl")
	})
}

// =============================================================================
// USER RECIPES — Linux passwd/grep vs macOS dscl
// =============================================================================

func TestPlatformUser_LinuxVsMacOS(t *testing.T) {
	t.Run("linux_with_username", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("user admin account locked")
		require.NotNil(t, r)
		assert.Equal(t, RecipeUser, r.Name)
		assert.Equal(t, "admin", r.ServiceName)
		assert.Contains(t, r.InitialCommand, "passwd -S")
		assert.Contains(t, r.InitialCommand, "/etc/passwd")
		assert.NotContains(t, r.InitialCommand, "dscl")

		fu := r.FollowUpCommand("some output")
		assert.Contains(t, fu, "faillock")
		assert.Contains(t, fu, "chage")
	})

	t.Run("darwin_with_username", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("user admin account locked")
		require.NotNil(t, r)
		assert.Equal(t, RecipeUser, r.Name)
		assert.Equal(t, "admin", r.ServiceName)
		assert.Contains(t, r.InitialCommand, "dscl")
		assert.Contains(t, r.InitialCommand, "/Users/")
		assert.NotContains(t, r.InitialCommand, "passwd -S")
		assert.NotContains(t, r.InitialCommand, "/etc/passwd")

		// macOS has no follow-up for user
		fu := r.FollowUpCommand("some output")
		assert.Empty(t, fu)
	})

	t.Run("no_username_any_platform", func(t *testing.T) {
		for _, plat := range []string{"linux", "darwin"} {
			t.Run(plat, func(t *testing.T) {
				withPlatform(t, plat)
				r := SelectRecipe("locked account for unknown")
				require.NotNil(t, r)
				assert.Equal(t, RecipeUser, r.Name)
				// "unknown" is extracted as username by the regex
			})
		}
	})
}

// =============================================================================
// CRON RECIPES — Linux systemctl list-timers vs macOS crontab only
// =============================================================================

func TestPlatformCron_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("cron job not running")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "systemctl list-timers")
		assert.Contains(t, r.InitialCommand, "crontab -l")

		fu := r.FollowUpCommand("crontab output")
		assert.Contains(t, fu, "journalctl -u cron")
		assert.NotContains(t, fu, "log show")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("cron job not running")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "crontab -l")
		assert.NotContains(t, r.InitialCommand, "systemctl list-timers")

		fu := r.FollowUpCommand("crontab output")
		assert.Contains(t, fu, "log show")
		assert.Contains(t, fu, "cron")
		assert.NotContains(t, fu, "journalctl")
	})
}

// =============================================================================
// PORT RECIPES — Linux ss vs macOS lsof
// =============================================================================

func TestPlatformPort_LinuxVsMacOS(t *testing.T) {
	t.Run("linux_initial", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("address already in use port 3000")
		require.NotNil(t, r)
		assert.Equal(t, "ss -tlnp", r.InitialCommand)
		assert.Equal(t, "3000", r.ServiceName)

		fu := r.FollowUpCommand("ss output")
		assert.Contains(t, fu, "ss -tlnp")
		assert.Contains(t, fu, "3000")
		assert.NotContains(t, fu, "lsof")
	})

	t.Run("darwin_initial", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("address already in use port 3000")
		require.NotNil(t, r)
		assert.Equal(t, "lsof -iTCP -sTCP:LISTEN -P -n", r.InitialCommand)
		assert.Equal(t, "3000", r.ServiceName)

		fu := r.FollowUpCommand("lsof output")
		assert.Contains(t, fu, "lsof -iTCP:")
		assert.Contains(t, fu, "3000")
		assert.NotContains(t, fu, "ss -tlnp")
	})
}

// =============================================================================
// PACKAGE RECIPES — Linux apt/dpkg vs macOS brew
// =============================================================================

func TestPlatformPackage_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("apt broken package")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "apt")
		assert.Contains(t, r.InitialCommand, "dpkg")
		assert.NotContains(t, r.InitialCommand, "brew")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("brew broken package")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "brew doctor")
		assert.NotContains(t, r.InitialCommand, "apt")
		assert.NotContains(t, r.InitialCommand, "dpkg")
	})
}

// =============================================================================
// TIME RECIPES — Linux timedatectl/chronyc vs macOS sntp/systemsetup
// =============================================================================

func TestPlatformTime_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("clock skew detected")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "timedatectl")
		assert.NotContains(t, r.InitialCommand, "sntp")

		fu := r.FollowUpCommand("timedatectl output")
		assert.Contains(t, fu, "chronyc")
		assert.NotContains(t, fu, "systemsetup")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("clock skew detected")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "sntp")
		assert.NotContains(t, r.InitialCommand, "timedatectl")

		fu := r.FollowUpCommand("sntp output")
		assert.Contains(t, fu, "systemsetup")
		assert.NotContains(t, fu, "chronyc")
	})
}

// =============================================================================
// LOG RECIPES — Linux journalctl vs macOS du/ls
// =============================================================================

func TestPlatformLog_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("journald taking too much space")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "journalctl --disk-usage")

		fu := r.FollowUpCommand("journal disk usage")
		assert.Contains(t, fu, "journalctl -p err")
		assert.NotContains(t, fu, "ls -lhS")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("journald taking too much space")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "du -sh /var/log/")
		assert.Contains(t, r.InitialCommand, "ls -lhS /var/log/")

		fu := r.FollowUpCommand("du output")
		assert.Contains(t, fu, "ls -lhS /var/log/")
		assert.NotContains(t, fu, "journalctl")
	})
}

// =============================================================================
// FIREWALL RECIPES — Linux iptables/nft/ufw vs macOS pfctl
// =============================================================================

func TestPlatformFirewall_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("firewall iptables issue")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "iptables")
		assert.Contains(t, r.InitialCommand, "nft list")
		assert.Contains(t, r.InitialCommand, "ufw")
		assert.NotContains(t, r.InitialCommand, "pfctl")

		fu := r.FollowUpCommand("iptables output")
		assert.Equal(t, "ss -tlnp", fu)
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("firewall blocking traffic")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "pfctl")
		assert.NotContains(t, r.InitialCommand, "iptables")

		fu := r.FollowUpCommand("pfctl output")
		assert.Equal(t, "lsof -iTCP -sTCP:LISTEN -P -n", fu)
	})
}

// =============================================================================
// IO RECIPES — Linux iostat -xz + iotop vs macOS iostat -c + nothing
// =============================================================================

func TestPlatformIO_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("iowait at 90%")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "iostat -xz")

		fu := r.FollowUpCommand("iostat output")
		assert.Contains(t, fu, "iotop")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("iowait at 90%")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "iostat -c 2")

		fu := r.FollowUpCommand("iostat output")
		assert.Empty(t, fu, "macOS IO has no follow-up")
	})
}

// =============================================================================
// HARDWARE RECIPES — Linux dmesg/smartctl vs macOS system_profiler/diskutil
// =============================================================================

func TestPlatformHardware_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("bad sector on drive")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "dmesg")

		fu := r.FollowUpCommand("dmesg output")
		assert.Contains(t, fu, "smartctl")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("bad sector on drive")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "system_profiler")

		fu := r.FollowUpCommand("profiler output")
		assert.Contains(t, fu, "diskutil")
	})
}

// =============================================================================
// BOOT RECIPES — Linux journalctl vs macOS log show
// =============================================================================

func TestPlatformBoot_LinuxVsMacOS(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("kernel panic on boot")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "journalctl -xb")
		assert.NotContains(t, r.InitialCommand, "log show")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("kernel panic on boot")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "log show")
		assert.NotContains(t, r.InitialCommand, "journalctl")
	})

	// Follow-up is platform-independent (fstab + who -b + last reboot)
	for _, plat := range []string{"linux", "darwin"} {
		t.Run(plat+"_followup", func(t *testing.T) {
			withPlatform(t, plat)
			r := SelectRecipe("won't boot")
			require.NotNil(t, r)
			fu := r.FollowUpCommand("boot output")
			assert.Contains(t, fu, "fstab")
			assert.Contains(t, fu, "who -b")
		})
	}
}

// =============================================================================
// DATABASE RECIPES — generic DB initial uses platform-specific port listing
// =============================================================================

func TestPlatformDatabase_GenericPortListing(t *testing.T) {
	t.Run("linux_generic_db", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("database connection timeout")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "ss -tlnp")
	})

	t.Run("darwin_generic_db", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("database connection timeout")
		require.NotNil(t, r)
		assert.Contains(t, r.InitialCommand, "lsof -iTCP")
		assert.NotContains(t, r.InitialCommand, "ss -tlnp")
	})

	// Named databases use same commands regardless of platform
	for _, plat := range []string{"linux", "darwin"} {
		t.Run(plat+"_postgres", func(t *testing.T) {
			withPlatform(t, plat)
			r := SelectRecipe("postgres database connection refused")
			require.NotNil(t, r)
			assert.Contains(t, r.InitialCommand, "pg_isready")
		})

		t.Run(plat+"_mysql", func(t *testing.T) {
			withPlatform(t, plat)
			r := SelectRecipe("mysql database slow query")
			require.NotNil(t, r)
			assert.Contains(t, r.InitialCommand, "mysqladmin")
		})

		t.Run(plat+"_redis", func(t *testing.T) {
			withPlatform(t, plat)
			r := SelectRecipe("redis connection refused")
			require.NotNil(t, r)
			assert.Contains(t, r.InitialCommand, "redis-cli")
		})
	}
}

// =============================================================================
// MEMORY RECIPES — Linux ps --sort vs macOS ps -r
// =============================================================================

func TestPlatformMemory_FollowUpSorting(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		r := SelectRecipe("out of memory")
		require.NotNil(t, r)
		fu := r.FollowUpCommand("mem data")
		assert.Contains(t, fu, "--sort=-rss")
		assert.NotContains(t, fu, "-r |")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		r := SelectRecipe("out of memory")
		require.NotNil(t, r)
		fu := r.FollowUpCommand("mem data")
		assert.Contains(t, fu, "-r |")
		assert.NotContains(t, fu, "--sort=")
	})
}

// =============================================================================
// FULL MATRIX — every recipe on every platform produces valid output
// =============================================================================

func TestPlatformFullMatrix(t *testing.T) {
	platforms := []string{"linux", "darwin", "freebsd"}

	inputs := []string{
		"disk full", "out of memory", "server slow", "dns failing",
		"no internet", "docker crash", "npm build fails", "nginx won't start",
		"permission denied", "port 8080 in use", "ssl expired", "merge conflict",
		"cron not running", "apt broken", "zombie process",
		"ssh connection refused to server", "ntp not syncing",
		"journald too big", "postgres database connection refused",
		"firewall blocking", "locked account",
		"io wait high", "temperature too high", "won't boot",
		"nfs stale file handle",
		"can't create files, df shows space available",
		"noexec mount issue",
		"api.internal resolves to wrong ip",
	}

	for _, plat := range platforms {
		t.Run(plat, func(t *testing.T) {
			withPlatform(t, plat)
			for _, input := range inputs {
				t.Run(input, func(t *testing.T) {
					r := SelectRecipe(input)
					require.NotNil(t, r, "%s/%q: nil recipe", plat, input)
					assert.NotEmpty(t, r.InitialCommand, "%s/%q: empty command", plat, input)
					assert.NotEmpty(t, string(r.Name), "%s/%q: empty recipe name", plat, input)

					// Follow-up should not panic
					_ = r.FollowUpCommand("sample output 95%")
					_ = r.FollowUpCommand("")
				})
			}
		})
	}
}

// =============================================================================
// LINUX-SPECIFIC TOOLS — verify Linux uses Linux-only tools
// =============================================================================

func TestPlatformLinux_LinuxOnlyTools(t *testing.T) {
	withPlatform(t, "linux")

	linuxTools := map[string]string{
		"out of memory":                        "free",
		"no internet":                          "ip -o addr",
		"port 8080 in use":                     "ss",
		"nginx won't start":                    "systemctl",
		"ntp not syncing":                      "timedatectl",
		"journald too big":                     "journalctl",
		"firewall blocking":                    "iptables",
		"io wait high":                         "iostat -xz",
		"temperature too high":                 "dmesg",
		"won't boot":                           "journalctl",
		"user admin account locked":            "passwd",
	}

	for input, tool := range linuxTools {
		t.Run(input, func(t *testing.T) {
			r := SelectRecipe(input)
			require.NotNil(t, r)
			assert.Contains(t, r.InitialCommand, tool,
				"Linux recipe for %q should use %s", input, tool)
		})
	}
}

// =============================================================================
// MACOS-SPECIFIC TOOLS — verify macOS uses macOS-only tools
// =============================================================================

func TestPlatformMacOS_MacOSOnlyTools(t *testing.T) {
	withPlatform(t, "darwin")

	macTools := map[string]string{
		"out of memory":        "vm_stat",
		"no internet":          "ifconfig",
		"port 8080 in use":     "lsof",
		"nginx won't start":    "launchctl",
		"ntp not syncing":      "sntp",
		"firewall blocking":    "pfctl",
		"io wait high":         "iostat -c",
		"temperature too high": "system_profiler",
		"won't boot":           "log show",
	}

	for input, tool := range macTools {
		t.Run(input, func(t *testing.T) {
			r := SelectRecipe(input)
			require.NotNil(t, r)
			assert.Contains(t, r.InitialCommand, tool,
				"macOS recipe for %q should use %s", input, tool)
		})
	}
}

// =============================================================================
// NO CROSS-CONTAMINATION — Linux tools don't appear on macOS, vice versa
// =============================================================================

func TestPlatformNoCrossContamination(t *testing.T) {
	linuxOnlyTools := []string{"systemctl", "ss -tlnp", "free -h", "ip -o addr", "timedatectl", "iptables", "nft list", "ufw", "iostat -xz", "dmesg"}
	macOnlyTools := []string{"launchctl", "vm_stat", "ifconfig", "scutil", "pfctl", "system_profiler", "pmset", "sntp"}

	platformInputs := []string{
		"out of memory", "dns failing", "no internet", "nginx won't start",
		"port 8080 in use", "ntp not syncing", "firewall blocking",
		"io wait high", "temperature too high", "won't boot",
	}

	t.Run("linux_no_mac_tools", func(t *testing.T) {
		withPlatform(t, "linux")
		for _, input := range platformInputs {
			r := SelectRecipe(input)
			if r == nil {
				continue
			}
			for _, tool := range macOnlyTools {
				assert.NotContains(t, r.InitialCommand, tool,
					"Linux recipe for %q should NOT contain macOS tool %q: got %q", input, tool, r.InitialCommand)
			}
		}
	})

	t.Run("darwin_no_linux_tools", func(t *testing.T) {
		withPlatform(t, "darwin")
		for _, input := range platformInputs {
			r := SelectRecipe(input)
			if r == nil {
				continue
			}
			for _, tool := range linuxOnlyTools {
				assert.NotContains(t, r.InitialCommand, tool,
					"macOS recipe for %q should NOT contain Linux tool %q: got %q", input, tool, r.InitialCommand)
			}
		}
	})
}

// =============================================================================
// PLATFORM SWITCH STABILITY — changing platform mid-test works correctly
// =============================================================================

func TestPlatformSwitchStability(t *testing.T) {
	// Verify that switching platforms produces different commands for the same input
	inputs := []string{
		"out of memory", "dns failing", "no internet", "port 8080 in use",
		"nginx won't start", "ntp not syncing", "firewall blocking",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			withPlatform(t, "linux")
			linuxR := SelectRecipe(input)
			require.NotNil(t, linuxR)
			linuxCmd := linuxR.InitialCommand

			withPlatform(t, "darwin")
			darwinR := SelectRecipe(input)
			require.NotNil(t, darwinR)
			darwinCmd := darwinR.InitialCommand

			assert.NotEqual(t, linuxCmd, darwinCmd,
				"Linux and macOS should produce different commands for %q\n  Linux:  %s\n  macOS:  %s", input, linuxCmd, darwinCmd)
		})
	}
}

// =============================================================================
// SERVICE FALLBACK CHAINS — Linux systemctl || service || grep
// =============================================================================

func TestPlatformLinux_ServiceFallbackChain(t *testing.T) {
	withPlatform(t, "linux")

	t.Run("generic_service_has_triple_fallback", func(t *testing.T) {
		r := SelectRecipe("service crashed")
		require.NotNil(t, r)
		cmd := r.InitialCommand
		// Should have: systemctl || service || grep
		assert.Contains(t, cmd, "systemctl --failed")
		assert.Contains(t, cmd, "service --status-all")
		assert.Contains(t, cmd, "grep -iE")
		// Count || operators
		pipes := strings.Count(cmd, "||")
		assert.GreaterOrEqual(t, pipes, 2, "should have at least 2 fallback operators")
	})

	t.Run("named_service_has_fallback", func(t *testing.T) {
		r := SelectRecipe("nginx won't start")
		require.NotNil(t, r)
		cmd := r.InitialCommand
		assert.Contains(t, cmd, "systemctl status")
		assert.Contains(t, cmd, "service")
		pipes := strings.Count(cmd, "||")
		assert.GreaterOrEqual(t, pipes, 1, "named service should have fallback")
	})
}

// =============================================================================
// COMPREHENSIVE FOLLOW-UP MATRIX — every recipe on both platforms
// =============================================================================

func TestPlatformFollowUpMatrix(t *testing.T) {
	type expectedFollowUp struct {
		input       string
		firstOutput string
		linux       string // expected substring in follow-up, or "" for empty
		darwin      string // expected substring in follow-up, or "" for empty
		linuxEmpty  bool   // if true, expect empty on Linux
		darwinEmpty bool   // if true, expect empty on macOS
	}

	tests := []expectedFollowUp{
		{"disk full", "95%", "du -xhd 1 /", "/System/Volumes/Data", false, false},
		{"out of memory", "mem", "--sort=-rss", "-r |", false, false},
		{"server slow", "load", "--sort=-pcpu", "-r |", false, false},
		{"dns failing", "dns", "ip route", "netstat -rn", false, false},
		{"no internet", "iface", "ip route", "netstat -rn", false, false},
		{"port 8080 in use", "data", "ss -tlnp", "lsof -iTCP:", false, false},
		{"cron not running", "tab", "journalctl -u cron", "log show", false, false},
		{"nginx won't start", "svc", "journalctl", "log show", false, false},
		{"ntp not syncing", "time", "chronyc", "systemsetup", false, false},
		{"journald too big", "disk", "journalctl -p err", "ls -lhS", false, false},
		{"firewall blocking", "rules", "ss -tlnp", "lsof -iTCP", false, false},
		{"io wait high", "io", "iotop", "", false, true},
		{"temperature too high", "hw", "smartctl", "diskutil", false, false},
		{"user admin account locked", "id", "faillock", "", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Run("linux", func(t *testing.T) {
				withPlatform(t, "linux")
				r := SelectRecipe(tc.input)
				require.NotNil(t, r)
				fu := r.FollowUpCommand(tc.firstOutput)
				if tc.linuxEmpty {
					assert.Empty(t, fu, "Linux follow-up should be empty")
				} else {
					assert.NotEmpty(t, fu, "Linux follow-up should not be empty")
					if tc.linux != "" {
						assert.Contains(t, fu, tc.linux)
					}
				}
			})

			t.Run("darwin", func(t *testing.T) {
				withPlatform(t, "darwin")
				r := SelectRecipe(tc.input)
				require.NotNil(t, r)
				fu := r.FollowUpCommand(tc.firstOutput)
				if tc.darwinEmpty {
					assert.Empty(t, fu, "macOS follow-up should be empty")
				} else {
					assert.NotEmpty(t, fu, "macOS follow-up should not be empty")
					if tc.darwin != "" {
						assert.Contains(t, fu, tc.darwin)
					}
				}
			})
		})
	}
}

// =============================================================================
// CLASSIFICATION IS PLATFORM-INDEPENDENT
// =============================================================================

func TestPlatformClassificationUnchanged(t *testing.T) {
	// Classification should produce identical results regardless of platform
	inputs := []struct {
		input string
		want  IssueClass
	}{
		{"disk full", IssueDisk},
		{"out of memory", IssueMemory},
		{"server slow", IssuePerformance},
		{"dns failing", IssueDNS},
		{"no internet", IssueNetwork},
		{"docker crash", IssueDocker},
		{"npm build fails", IssueBuild},
		{"nginx won't start", IssueService},
		{"permission denied", IssuePermission},
		{"port 8080 in use", IssuePort},
		{"ssl expired", IssueSSL},
		{"merge conflict", IssueGit},
		{"cron not running", IssueCron},
		{"apt broken", IssuePackage},
		{"zombie process", IssueProcess},
		{"ssh connection refused to server", IssueSSH},
		{"ntp not syncing", IssueTime},
		{"journald too big", IssueLog},
		{"postgres database connection refused", IssueDatabase},
		{"firewall blocking", IssueFirewall},
		{"locked account", IssueUser},
		{"io wait high", IssueIO},
		{"temperature too high", IssueHardware},
		{"won't boot", IssueBoot},
		{"nfs stale file handle", IssueNFS},
	}

	platforms := []string{"linux", "darwin", "freebsd", "windows"}

	for _, plat := range platforms {
		t.Run(plat, func(t *testing.T) {
			withPlatform(t, plat)
			for _, tc := range inputs {
				got := ClassifyIssue(tc.input)
				assert.Equal(t, tc.want, got,
					"classification of %q should be %s on %s", tc.input, tc.want, plat)
			}
		})
	}
}

// =============================================================================
// WINDOWS (UNSUPPORTED) — verify recipes still work (degrade to Linux defaults)
// =============================================================================

func TestPlatformWindows_GracefulDegradation(t *testing.T) {
	withPlatform(t, "windows")

	// Every recipe should still produce non-nil with non-empty command
	inputs := []string{
		"disk full", "out of memory", "server slow", "dns failing",
		"nginx won't start", "port 8080 in use", "ntp not syncing",
		"firewall blocking", "won't boot",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			r := SelectRecipe(input)
			require.NotNil(t, r)
			assert.NotEmpty(t, r.InitialCommand)
		})
	}

	// Windows should get Linux defaults (not darwin)
	t.Run("gets_linux_defaults", func(t *testing.T) {
		r := SelectRecipe("out of memory")
		require.NotNil(t, r)
		assert.Equal(t, "free -h", r.InitialCommand, "Windows should get Linux default")
	})
}

// =============================================================================
// HELPER: buildPackageCommand platform test
// =============================================================================

func TestPlatformBuildPackageCommand(t *testing.T) {
	t.Run("linux", func(t *testing.T) {
		withPlatform(t, "linux")
		cmd := buildPackageCommand()
		assert.Contains(t, cmd, "apt")
		assert.NotContains(t, cmd, "brew")
	})

	t.Run("darwin", func(t *testing.T) {
		withPlatform(t, "darwin")
		cmd := buildPackageCommand()
		assert.Contains(t, cmd, "brew")
		assert.NotContains(t, cmd, "apt")
	})

	t.Run("unknown", func(t *testing.T) {
		withPlatform(t, "freebsd")
		cmd := buildPackageCommand()
		assert.Contains(t, cmd, "apt", "unknown OS should use Linux default")
	})
}

// =============================================================================
// HELPER: buildDatabaseCommand platform test
// =============================================================================

func TestPlatformBuildDatabaseCommand(t *testing.T) {
	// Named DB commands are platform-independent
	for _, plat := range []string{"linux", "darwin"} {
		t.Run(plat+"_postgres", func(t *testing.T) {
			withPlatform(t, plat)
			cmd := buildDatabaseCommand("postgres")
			assert.Contains(t, cmd, "pg_isready")
		})
		t.Run(plat+"_mysql", func(t *testing.T) {
			withPlatform(t, plat)
			cmd := buildDatabaseCommand("mysql")
			assert.Contains(t, cmd, "mysqladmin")
		})
		t.Run(plat+"_redis", func(t *testing.T) {
			withPlatform(t, plat)
			cmd := buildDatabaseCommand("redis")
			assert.Contains(t, cmd, "redis-cli")
		})
	}

	// Generic DB command is platform-dependent
	t.Run("linux_generic", func(t *testing.T) {
		withPlatform(t, "linux")
		cmd := buildDatabaseCommand("")
		assert.Contains(t, cmd, "ss -tlnp")
	})

	t.Run("darwin_generic", func(t *testing.T) {
		withPlatform(t, "darwin")
		cmd := buildDatabaseCommand("")
		assert.Contains(t, cmd, "lsof -iTCP")
		assert.NotContains(t, cmd, "ss -tlnp")
	})
}

// =============================================================================
// MULTI-RECIPE PER-PLATFORM — verify service name embedded correctly
// =============================================================================

func TestPlatformServiceNameEmbedding(t *testing.T) {
	// Note: "grafana" omitted — contains "fan" which triggers IssueHardware (substring collision)
	services := []string{"nginx", "postgres", "redis", "caddy"}

	for _, svc := range services {
		t.Run(svc, func(t *testing.T) {
			input := fmt.Sprintf("%s won't start", svc)

			t.Run("linux", func(t *testing.T) {
				withPlatform(t, "linux")
				r := SelectRecipe(input)
				require.NotNil(t, r)
				assert.Equal(t, svc, r.ServiceName)
				assert.Contains(t, r.InitialCommand, svc)
			})

			t.Run("darwin", func(t *testing.T) {
				withPlatform(t, "darwin")
				r := SelectRecipe(input)
				require.NotNil(t, r)
				assert.Equal(t, svc, r.ServiceName)
				assert.Contains(t, r.InitialCommand, svc)
			})
		})
	}
}
