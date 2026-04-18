package diagnose

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// REAL-WORLD MESSY USER INPUT — typos, verbose, terse, colloquial, mixed case
// =============================================================================

func TestBroadMessyUserInput(t *testing.T) {
	type tc struct {
		input string
		want  IssueClass
		note  string
	}

	cases := []tc{
		// Typos and misspellings
		{"permision denied on my file", IssueGeneral, "misspelled permission should not match"},
		{"PERMISSION DENIED ON /var/log", IssuePermission, "all caps should match"},
		{"Permission Denied writing config", IssuePermission, "mixed case should match"},

		// Very verbose descriptions
		{"so basically what happened is that my disk is completely full and I can't even write a single file anymore because there's no space left on the device at all", IssueDisk, "long verbose input with disk keywords"},
		{"I was trying to run my node application and it said that the address is already in use for port 3000 and now I can't start anything", IssuePort, "verbose port conflict"},
		{"I noticed that when I try to connect via ssh to my server it keeps telling me the host key has changed and I don't know what to do", IssueSSH, "verbose SSH description"},

		// Very terse
		{"oom", IssueMemory, "just 'oom'"},
		{"enospc", IssueDisk, "just 'enospc'"},
		{"zombie", IssueProcess, "just 'zombie'"},
		{"swap", IssueMemory, "just 'swap'"},

		// Colloquial/informal
		{"my box is running like a dog", IssueGeneral, "colloquial with no keywords"},
		{"nginx took a dump", IssueService, "colloquial with service name"},
		{"this thing just won't boot up anymore", IssueBoot, "informal boot description"},
		{"ran out of space again ugh", IssueDisk, "frustrated user disk issue"},
		{"can't pip install anything", IssuePackage, "informal package issue"},

		// Error codes as input
		{"EACCES", IssuePermission, "POSIX error code"},
		{"EMFILE", IssueProcess, "file descriptor limit error code"},
		{"EADDRINUSE", IssuePort, "address in use error code"},

		// Multi-line input (newlines in string)
		{"Error: ENOSPC: no space left on device\n  at /app/server.js:42", IssueDisk, "multi-line error with stack trace"},
		{"fatal: unable to access 'https://github.com/foo/bar.git'\n  Could not resolve host: github.com", IssueDNS, "git error that's really DNS"},

		// Leading/trailing whitespace
		{"   disk full   ", IssueDisk, "whitespace padding"},
		{"\n\nssl cert expired\n\n", IssueSSL, "newline padding"},
	}

	pass, fail := 0, 0
	var failures []string
	for _, c := range cases {
		got := ClassifyIssue(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → got %s, want %s (%s)", c.input, got, c.want, c.note))
		}
	}

	t.Logf("\n=== MESSY INPUT: %d/%d (%.0f%%) ===", pass, pass+fail, pct(pass, fail))
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d messy input failures", fail)
	}
}

// =============================================================================
// MULTI-SIGNAL INPUTS — 3+ class keywords where priority must resolve
// =============================================================================

func TestBroadMultiSignalPriority(t *testing.T) {
	type tc struct {
		input string
		want  IssueClass
		note  string
	}

	cases := []tc{
		// permission + disk + service
		{"permission denied writing to disk, nginx service failing", IssuePermission, "permission wins over disk and service"},
		// ssl + docker + build
		{"ssl certificate error in docker build", IssueSSL, "ssl wins over docker and build"},
		// port + docker + network
		{"docker container port 8080 address already in use", IssuePort, "port conflict wins over docker"},
		// cron + permission + service
		{"cron job permission denied on script", IssuePermission, "permission wins over cron"},
		// memory + docker + performance — "oom" triggers memory before docker in classification order
		{"docker container oom killed very slow", IssueMemory, "oom triggers memory before docker"},
		// git + build
		{"git pull fails during npm build", IssueGit, "git wins over build (checked first)"},
		// nfs + network + mount
		{"nfs mount over network is timing out", IssueNFS, "nfs wins over network"},
		// firewall + network + port
		{"firewall blocking network port", IssueFirewall, "firewall wins over network"},
		// disk + io
		{"disk slow io wait high and space low", IssueIO, "io wait is more specific than disk"},
		// ssh + permission
		{"ssh permission denied publickey", IssuePermission, "permission denied triggers permission before ssh"},
		// hardware + boot — "boot" keyword is checked before "hardware error" in switch order
		{"hardware error on boot", IssueBoot, "boot is checked before hardware error"},
	}

	pass, fail := 0, 0
	var failures []string
	for _, c := range cases {
		got := ClassifyIssue(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → got %s, want %s (%s)", c.input, got, c.want, c.note))
		}
	}

	t.Logf("\n=== MULTI-SIGNAL PRIORITY: %d/%d (%.0f%%) ===", pass, pass+fail, pct(pass, fail))
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d multi-signal priority failures", fail)
	}
}

// =============================================================================
// NEGATIVE TESTS — inputs that look like a class but shouldn't match
// =============================================================================

func TestBroadNegativeClassification(t *testing.T) {
	type tc struct {
		input    string
		dontWant IssueClass
		note     string
	}

	cases := []tc{
		// "port" in non-conflict context — correctly avoids IssuePort (needs conflict keywords)
		{"network port scan shows issues", IssuePort, "port scan is network, not port conflict"},
		// "time" in generic context — correctly avoids IssueTime (needs clock/ntp qualifier)
		{"this takes too much time", IssueTime, "time without clock/ntp context is not IssueTime"},
		// "log" without management context — correctly avoids IssueLog
		{"check the log for errors", IssueLog, "generic log reference is not log management"},
		// "mount" without NFS/failure context — correctly avoids IssueNFS
		{"mount everest photos", IssueNFS, "mount in non-technical context"},
		// "ssh" without auth/connect context — correctly avoids IssueSSH
		{"I have an ssh directory in my home", IssueSSH, "just mentioning ssh directory"},
	}

	pass, fail := 0, 0
	var failures []string
	for _, c := range cases {
		got := ClassifyIssue(c.input)
		if got != c.dontWant {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → got %s, should NOT be %s (%s)", c.input, got, c.dontWant, c.note))
		}
	}

	t.Logf("\n=== NEGATIVE TESTS: %d/%d (%.0f%%) ===", pass, pass+fail, pct(pass, fail))
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d negative classification failures", fail)
	}
}

// =============================================================================
// RECIPE COMMAND SAFETY — no destructive commands in any recipe
// =============================================================================

func TestBroadRecipeCommandSafety(t *testing.T) {
	destructivePatterns := []string{
		`\brm\s+-rf\b`,
		`\brm\s+-r\b`,
		`\brm\s+/`,
		`\bdd\s+if=`,
		`\bmkfs\b`,
		`\bfdisk\b`,
		`\bshred\b`,
		`\bwipe\b`,
		`\bmkfs\.\w+\b`,  // mkfs.ext4, mkfs.xfs, etc.
		`\bdrop\s+database\b`,
		`\btruncate\b`,
		`\bkill\s+-9\b`,
		`\bkillall\b`,
		`\bpkill\b`,
		`\bchmod\s+-R\s+777\b`,
		`\bchown\s+-R\b`,
		`\biptables\s+-F\b`,       // flush rules
		`\biptables\s+--flush\b`,
		`\bsystemctl\s+stop\b`,
		`\bsystemctl\s+restart\b`,
		`\bsystemctl\s+disable\b`,
		`\bservice\s+\S+\s+stop\b`,
		`\bservice\s+\S+\s+restart\b`,
		`(?:^|;)\s*reboot\b`,  // "reboot" as a command, not in "last reboot"
		`\bshutdown\b`,
		`\binit\s+0\b`,
		`\bpoweroff\b`,
	}

	compiled := make([]*regexp.Regexp, len(destructivePatterns))
	for i, p := range destructivePatterns {
		compiled[i] = regexp.MustCompile(p)
	}

	// Every classifiable input that produces a recipe
	inputs := []string{
		"disk full", "out of memory", "server slow", "dns failing",
		"no internet", "docker crashing", "npm build fails", "nginx won't start",
		"permission denied on /etc/foo/bar", "port 8080 in use", "ssl expired",
		"merge conflict", "cron not running", "apt broken package", "zombie process",
		"can't create files, df shows space available",
		"noexec mount on /tmp", "api.internal resolves to wrong ip",
		"ssh connection refused to server", "ntp not syncing",
		"journald too big", "postgres database connection refused",
		"firewall blocking traffic", "locked account for admin",
		"io wait high", "temperature too high", "won't boot",
		"nfs stale file handle", "too many open files ulimit",
		"redis connection refused", "mysql database slow query",
	}

	pass, fail := 0, 0
	var failures []string

	for _, input := range inputs {
		r := SelectRecipe(input)
		if r == nil {
			continue
		}

		cmds := []string{r.InitialCommand}
		fu := r.FollowUpCommand("sample output here with some data 90%")
		if fu != "" {
			cmds = append(cmds, fu)
		}

		for _, cmd := range cmds {
			for _, re := range compiled {
				if re.MatchString(cmd) {
					fail++
					failures = append(failures, fmt.Sprintf("  DESTRUCTIVE: %q recipe=%s cmd=%q matches %s", input, r.Name, cmd, re.String()))
				} else {
					pass++
				}
			}
		}
	}

	t.Logf("\n=== COMMAND SAFETY: %d/%d checks passed ===", pass, pass+fail)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d destructive command patterns found in recipes", fail)
	}
}

// =============================================================================
// FULL PIPELINE ROUND-TRIP — classify → recipe → initial cmd → follow-up
// for every issue class
// =============================================================================

func TestBroadFullPipelineRoundTrip(t *testing.T) {
	// One representative input per issue class
	classInputs := map[IssueClass]string{
		IssueDisk:        "disk space full",
		IssueMemory:      "out of memory",
		IssuePerformance: "server very slow",
		IssueDNS:         "dns resolution failing",
		IssueNetwork:     "no internet",
		IssueDocker:      "docker container crashing",
		IssueBuild:       "npm build fails",
		IssueService:     "nginx won't start",
		IssuePermission:  "permission denied on /var/log/app.log",
		IssuePort:        "address already in use port 8080",
		IssueSSL:         "ssl certificate expired",
		IssueGit:         "merge conflict in main.go",
		IssueCron:        "cron job not running",
		IssuePackage:     "apt broken package",
		IssueProcess:     "zombie processes",
		IssueSSH:         "ssh connection refused to server",
		IssueTime:        "ntp not syncing",
		IssueLog:         "journald taking too much space",
		IssueDatabase:    "postgres database connection refused",
		IssueFirewall:    "firewall blocking traffic",
		IssueUser:        "locked account for admin",
		IssueIO:          "io wait too high",
		IssueHardware:    "temperature too high",
		IssueBoot:        "server won't boot",
		IssueNFS:         "nfs stale file handle",
	}

	for wantClass, input := range classInputs {
		t.Run(string(wantClass), func(t *testing.T) {
			// Step 1: Classification
			gotClass := ClassifyIssue(input)
			assert.Equal(t, wantClass, gotClass, "classification mismatch for %q", input)

			// Step 2: Recipe selection
			recipe := SelectRecipe(input)
			require.NotNil(t, recipe, "nil recipe for %q (class %s)", input, wantClass)

			// Step 3: Recipe has correct IssueClass
			assert.Equal(t, wantClass, recipe.IssueClass, "recipe.IssueClass mismatch")

			// Step 4: InitialCommand is non-empty
			assert.NotEmpty(t, recipe.InitialCommand, "empty InitialCommand for %s", recipe.Name)

			// Step 5: InitialCommand doesn't start with space or semicolon
			assert.False(t, strings.HasPrefix(recipe.InitialCommand, " "), "InitialCommand starts with space")
			assert.False(t, strings.HasPrefix(recipe.InitialCommand, ";"), "InitialCommand starts with semicolon")

			// Step 6: Follow-up with non-empty output should not panic
			_ = recipe.FollowUpCommand("some output data here")

			// Step 7: Follow-up with empty output should be empty
			assert.Empty(t, recipe.FollowUpCommand(""), "follow-up should be empty for empty first output")
			assert.Empty(t, recipe.FollowUpCommand("   "), "follow-up should be empty for whitespace first output")

			// Step 8: Fact collectors exist and don't panic
			plan := planFactsForIssue(wantClass)
			assert.NotEmpty(t, plan, "no fact collectors for %s", wantClass)
		})
	}
}

// =============================================================================
// DATABASE SUBCATEGORY ROUTING — postgres vs mysql vs redis vs generic
// =============================================================================

func TestBroadDatabaseSubcategoryRouting(t *testing.T) {
	tests := []struct {
		input       string
		wantService string
		wantCmdSub  string
	}{
		{"postgres database connection refused", "postgres", "pg_isready"},
		{"postgresql database connection issues", "postgresql", "pg_isready"},
		{"mysql database slow query performance", "mysql", "mysqladmin"},
		{"mariadb database connection timeout", "mariadb", "mysqladmin"},
		{"redis connection refused to cache", "redis", "redis-cli"},
		{"database connection timeout", "", "5432"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r, "nil recipe")
			assert.Equal(t, RecipeDatabase, r.Name)
			assert.Equal(t, tc.wantService, r.ServiceName, "wrong ServiceName")
			assert.Contains(t, r.InitialCommand, tc.wantCmdSub, "command missing expected substring")
		})
	}
}

// =============================================================================
// DISK USAGE THRESHOLD BOUNDARY — 84% vs 85% vs 95%
// =============================================================================

func TestBroadDiskUsageThreshold(t *testing.T) {
	recipe := SelectRecipe("disk usage check")
	require.NotNil(t, recipe)
	assert.Equal(t, RecipeDiskUsage, recipe.Name)

	tests := []struct {
		output    string
		wantEmpty bool
		note      string
	}{
		{"Use% 84%", true, "84% should not trigger follow-up"},
		{"Use% 85%", false, "85% should trigger follow-up"},
		{"Use% 86%", false, "86% should trigger follow-up"},
		{"Use% 95%", false, "95% should trigger follow-up"},
		{"Use% 99%", false, "99% should trigger follow-up"},
		{"Use% 100%", false, "100% should trigger follow-up"},
		{"Use% 0%", true, "0% should not trigger follow-up"},
		{"Use% 50%", true, "50% should not trigger follow-up"},
		// Multiple percentages — one above threshold should trigger
		{"sda1 40% sda2 90%", false, "one partition above threshold should trigger"},
		// No percentage at all
		{"no numbers here", true, "no percentages should not trigger"},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			followUp := recipe.FollowUpCommand(tc.output)
			if tc.wantEmpty {
				assert.Empty(t, followUp, tc.note)
			} else {
				assert.NotEmpty(t, followUp, tc.note)
			}
		})
	}
}

// =============================================================================
// EDGE INPUT — empty, whitespace, very long, special chars, unicode
// =============================================================================

func TestBroadEdgeInputs(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		assert.Equal(t, IssueGeneral, ClassifyIssue(""))
		assert.Nil(t, SelectRecipe("")) // general returns nil from SelectRecipe
	})

	t.Run("whitespace only", func(t *testing.T) {
		assert.Equal(t, IssueGeneral, ClassifyIssue("   "))
		assert.Equal(t, IssueGeneral, ClassifyIssue("\t\n"))
	})

	t.Run("very long input", func(t *testing.T) {
		long := strings.Repeat("the server is having issues ", 200) + "disk full"
		got := ClassifyIssue(long)
		assert.Equal(t, IssueDisk, got, "should still find 'disk full' in long input")
	})

	t.Run("special characters", func(t *testing.T) {
		assert.Equal(t, IssueDisk, ClassifyIssue("disk full!!! *** $$$"))
		assert.Equal(t, IssuePermission, ClassifyIssue("permission denied: /var/log/app's file"))
	})

	t.Run("unicode input", func(t *testing.T) {
		// Should not panic, should fall through to general
		got := ClassifyIssue("serveur lent 慢い サーバー")
		assert.Equal(t, IssueGeneral, got)
	})

	t.Run("numbers only", func(t *testing.T) {
		assert.Equal(t, IssueGeneral, ClassifyIssue("12345"))
	})

	t.Run("single character", func(t *testing.T) {
		assert.Equal(t, IssueGeneral, ClassifyIssue("x"))
	})
}

// =============================================================================
// SHELL ESCAPE — verify paths with special chars are properly escaped
// =============================================================================

func TestBroadShellEscape(t *testing.T) {
	tests := []struct {
		input  string
		escape string
		note   string
	}{
		{"hello", "'hello'", "simple string"},
		{"it's", `'it'"'"'s'`, "single quote in string"},
		{"/var/log/app.log", "'/var/log/app.log'", "path"},
		{"foo bar", "'foo bar'", "space in string"},
		{"$(whoami)", "'$(whoami)'", "command substitution should be quoted"},
		{"`id`", "'`id`'", "backtick command should be quoted"},
		{"a;rm -rf /", "'a;rm -rf /'", "injection attempt should be quoted"},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			got := shellEscapeToken(tc.input)
			assert.Equal(t, tc.escape, got)
		})
	}
}

// =============================================================================
// RECIPE PROPERTIES — IssueClass field always matches classification
// =============================================================================

func TestBroadRecipeIssueClassConsistency(t *testing.T) {
	inputs := []string{
		"disk full", "out of memory", "system slow", "dns failing",
		"network down", "docker crash", "npm build error", "nginx crashed",
		"permission denied", "port 8080 in use", "ssl expired", "merge conflict",
		"cron not running", "apt broken", "zombie process",
		"ssh connection refused to server", "ntp wrong", "journald huge",
		"postgres database connection refused", "firewall iptables issue",
		"locked account", "io wait high", "thermal throttling",
		"won't boot", "nfs stale file handle",
	}

	for _, input := range inputs {
		r := SelectRecipe(input)
		if r == nil {
			continue
		}
		t.Run(input, func(t *testing.T) {
			classified := ClassifyIssue(input)
			assert.Equal(t, classified, r.IssueClass,
				"recipe IssueClass (%s) doesn't match ClassifyIssue result (%s) for %q",
				r.IssueClass, classified, input)
		})
	}
}

// =============================================================================
// KNOWLEDGE QUERY BYPASS — broader set including edge cases
// =============================================================================

func TestBroadKnowledgeBypass(t *testing.T) {
	// Should bypass (return nil from SelectRecipe)
	bypass := []string{
		"what is a segfault",
		"what are inodes",
		"what does the OOM killer do",
		"explain TCP handshake",
		"define swap space",
		"describe how cgroups work",
		"what is systemd",
		"explain the difference between tcp and udp",
	}

	// Should NOT bypass even though they contain knowledge-like words
	noBypas := []string{
		"my disk is full, what should I do",
		"nginx what is going on with it",
		"dns explain why it's broken", // doesn't start with "explain"
		"memory is high, describe the issue please",
	}

	for _, q := range bypass {
		t.Run("bypass/"+q, func(t *testing.T) {
			assert.Nil(t, SelectRecipe(q), "should return nil for knowledge query")
		})
	}

	for _, q := range noBypas {
		t.Run("no_bypass/"+q, func(t *testing.T) {
			assert.NotNil(t, SelectRecipe(q), "should return recipe for action query")
		})
	}
}

// =============================================================================
// EXTRACTOR EDGE CASES — boundary conditions, unicode, multiple matches
// =============================================================================

func TestBroadExtractorEdgeCases(t *testing.T) {
	t.Run("ExtractHostname edge cases", func(t *testing.T) {
		// Multiple hostnames — returns first match
		got := ExtractHostname("both api.internal and db.external are down")
		assert.NotEmpty(t, got, "should extract at least one hostname")

		// IP-like patterns are not hostnames (no alpha component at start)
		got = ExtractHostname("192.168.1.1 is unreachable")
		// IPs match the regex since segments are alphanumeric; this is expected
		// What matters is single words don't match
		got = ExtractHostname("localhost is down")
		assert.Empty(t, got, "single word should not match hostname regex")

		// Very long hostname
		got = ExtractHostname("app.region.cluster.env.company.internal is slow")
		assert.Equal(t, "app.region.cluster.env.company.internal", got)
	})

	t.Run("ExtractDatabaseType edge cases", func(t *testing.T) {
		// postgresql vs postgres
		assert.Equal(t, "postgresql", ExtractDatabaseType("postgresql is down"))
		assert.Equal(t, "postgres", ExtractDatabaseType("postgres is down"))

		// mongodb vs mongo
		assert.Equal(t, "mongodb", ExtractDatabaseType("mongodb cluster failing"))
		assert.Equal(t, "mongo", ExtractDatabaseType("mongo shell not connecting"))

		// Case insensitive
		assert.Equal(t, "redis", ExtractDatabaseType("REDIS is not responding"))

		// No match
		assert.Empty(t, ExtractDatabaseType("the database is slow"))
	})

	t.Run("ExtractUsername edge cases", func(t *testing.T) {
		// Quoted username
		assert.Equal(t, "deploy", ExtractUsername("user 'deploy' cannot login"))
		assert.Equal(t, "admin", ExtractUsername(`account "admin" is locked`))

		// False positives
		assert.Empty(t, ExtractUsername("user is not working"))
		assert.Empty(t, ExtractUsername("user the system"))
		assert.Empty(t, ExtractUsername("the user cannot do anything"))
		assert.Empty(t, ExtractUsername("user my account is broken"))

		// Valid usernames with underscores/dashes
		assert.Equal(t, "web_deploy", ExtractUsername("user web_deploy cannot login"))
		assert.Equal(t, "app-user", ExtractUsername("account app-user is locked"))
	})

	t.Run("ExtractPort edge cases", func(t *testing.T) {
		// Single digit port should not match (regex requires 2+)
		assert.Empty(t, ExtractPort("port 1"))

		// 5 digit port
		assert.Equal(t, "65535", ExtractPort("port 65535 in use"))

		// Multiple ports — returns first
		got := ExtractPort("ports 8080 and 3000 both busy")
		assert.NotEmpty(t, got)
	})

	t.Run("ExtractServiceName edge cases", func(t *testing.T) {
		// Known service names (direct match)
		assert.Equal(t, "nginx", ExtractServiceName("nginx"))
		assert.Equal(t, "docker", ExtractServiceName("docker"))

		// Fallback pattern with false positives
		assert.Empty(t, ExtractServiceName("the service is down"))
		assert.Empty(t, ExtractServiceName("a daemon crashed"))

		// Hyphenated service name via fallback
		assert.Equal(t, "my-app", ExtractServiceName("my-app service won't start"))
	})

	t.Run("ExtractPath edge cases", func(t *testing.T) {
		// Relative path
		got := ExtractPath("error in src/main.go file")
		assert.Equal(t, "src/main.go", got)

		// No path
		assert.Empty(t, ExtractPath("some error happened"))

		// Deep path
		got = ExtractPath("can't read /very/deep/nested/path/to/file.conf")
		assert.Equal(t, "/very/deep/nested/path/to/file.conf", got)
	})

	t.Run("ExtractBuildTool edge cases", func(t *testing.T) {
		// cmake (new keyword)
		assert.Equal(t, "cmake", ExtractBuildTool("cmake build failing"))

		// Multiple tools — returns first
		got := ExtractBuildTool("npm and webpack both failing")
		assert.NotEmpty(t, got)

		// No match
		assert.Empty(t, ExtractBuildTool("the build is broken"))
	})
}

// =============================================================================
// CLASSIFICATION STABILITY — same input should always give same result
// =============================================================================

func TestBroadClassificationDeterminism(t *testing.T) {
	inputs := []string{
		"disk full", "permission denied", "docker crash", "ssl expired",
		"nginx won't start", "oom killed", "ntp drift", "ssh timeout",
		"firewall iptables rules", "io wait high", "grub error",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			first := ClassifyIssue(input)
			for i := 0; i < 100; i++ {
				assert.Equal(t, first, ClassifyIssue(input), "classification should be deterministic")
			}
		})
	}
}

// =============================================================================
// ALL ISSUE CLASSES HAVE RECIPES — no classification results in nil
// =============================================================================

func TestBroadAllClassesProduceRecipes(t *testing.T) {
	allClasses := []IssueClass{
		IssueDisk, IssueMemory, IssuePerformance, IssueDNS, IssueNetwork,
		IssueDocker, IssueBuild, IssueService, IssuePermission, IssuePort,
		IssueSSL, IssueGit, IssueCron, IssuePackage, IssueProcess,
		IssueSSH, IssueTime, IssueLog, IssueDatabase, IssueFirewall,
		IssueUser, IssueIO, IssueHardware, IssueBoot, IssueNFS,
	}

	// Map each class to a known trigger input
	classTriggers := map[IssueClass]string{
		IssueDisk:        "disk full",
		IssueMemory:      "out of memory",
		IssuePerformance: "slow server",
		IssueDNS:         "dns resolution failing",
		IssueNetwork:     "no internet connectivity",
		IssueDocker:      "docker container crashed",
		IssueBuild:       "npm build fails",
		IssueService:     "nginx won't start",
		IssuePermission:  "permission denied",
		IssuePort:        "address already in use port 8080",
		IssueSSL:         "ssl certificate expired",
		IssueGit:         "merge conflict",
		IssueCron:        "cron job not running",
		IssuePackage:     "apt broken",
		IssueProcess:     "zombie process",
		IssueSSH:         "ssh connection refused to server",
		IssueTime:        "ntp not syncing",
		IssueLog:         "journald too big",
		IssueDatabase:    "postgres database connection refused",
		IssueFirewall:    "firewall blocking",
		IssueUser:        "locked account",
		IssueIO:          "io wait high",
		IssueHardware:    "temperature too high",
		IssueBoot:        "won't boot",
		IssueNFS:         "nfs stale file handle",
	}

	for _, cls := range allClasses {
		t.Run(string(cls), func(t *testing.T) {
			input, ok := classTriggers[cls]
			require.True(t, ok, "missing trigger for %s", cls)

			// Verify classification
			got := ClassifyIssue(input)
			assert.Equal(t, cls, got, "trigger %q should classify as %s", input, cls)

			// Verify recipe
			recipe := SelectRecipe(input)
			require.NotNil(t, recipe, "nil recipe for class %s", cls)
			assert.NotEmpty(t, recipe.InitialCommand)
			assert.NotEmpty(t, string(recipe.Name))
		})
	}
}

// =============================================================================
// SERVICE NAME FALLBACK CHAINS — systemctl || service || grep
// =============================================================================

func TestBroadServiceFallbackChains(t *testing.T) {
	// Generic (no service name) — "failed to start" with "daemon" triggers service
	r := SelectRecipe("a daemon failed to start and is crashing")
	require.NotNil(t, r)
	assert.Equal(t, RecipeServiceFailure, r.Name)
	// Should contain fallback indicators (|| or multiple commands)
	cmd := r.InitialCommand
	assert.True(t,
		strings.Contains(cmd, "||") || strings.Contains(cmd, "2>/dev/null") || strings.Contains(cmd, "launchctl"),
		"generic service command should have fallbacks: %s", cmd)

	// Named service should also produce a recipe
	r2 := SelectRecipe("redis daemon keeps crashing")
	require.NotNil(t, r2)
	assert.Equal(t, RecipeServiceFailure, r2.Name)
	assert.Equal(t, "redis", r2.ServiceName)
}

// =============================================================================
// FOLLOW-UP COMPLETENESS — verify important recipes have follow-ups
// =============================================================================

func TestBroadFollowUpCompleteness(t *testing.T) {
	type tc struct {
		input     string
		hasFollowUp bool
		note      string
	}

	tests := []tc{
		// Recipes that SHOULD have follow-ups
		{"server very slow", true, "performance always has follow-up"},
		{"out of memory", true, "memory always has follow-up"},
		{"dns failing", true, "dns always has follow-up"},
		{"no internet", true, "network always has follow-up"},
		{"merge conflict", true, "git always has follow-up"},
		{"ssl expired", true, "ssl always has follow-up"},
		{"cron not running", true, "cron always has follow-up"},
		{"zombie process", true, "process always has follow-up"},
		{"ssh connection refused to server", true, "ssh always has follow-up"},
		{"ntp not syncing", true, "time always has follow-up"},
		{"journald too big", true, "log always has follow-up"},
		{"postgres database connection refused", true, "database always has follow-up"},
		{"firewall blocking", true, "firewall always has follow-up"},
		{"won't boot", true, "boot always has follow-up"},
		{"nfs stale file handle", true, "nfs always has follow-up"},

		// Recipes that should NOT have follow-ups (or conditional)
		{"noexec mount issue", false, "permission_mount has no follow-up"},
		{"api.internal resolves to wrong ip", false, "dns_hosts has no follow-up"},
		{"npm build fails", false, "build has no follow-up"},
		{"apt broken package", false, "package has no follow-up"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r, "nil recipe")
			fu := r.FollowUpCommand("some output data")
			if tc.hasFollowUp {
				assert.NotEmpty(t, fu, "%s: expected follow-up", tc.note)
			} else {
				assert.Empty(t, fu, "%s: expected no follow-up", tc.note)
			}
		})
	}
}

// =============================================================================
// PROCESS SUBCATEGORY — zombie vs file descriptor
// =============================================================================

func TestBroadProcessSubcategory(t *testing.T) {
	// Zombie/defunct route
	r := SelectRecipe("zombie processes everywhere")
	require.NotNil(t, r)
	assert.Contains(t, r.InitialCommand, "defunct")

	// File descriptor route
	r2 := SelectRecipe("too many open files emfile")
	require.NotNil(t, r2)
	assert.Contains(t, r2.InitialCommand, "ulimit")

	// fd leak
	r3 := SelectRecipe("fd leak in my application")
	require.NotNil(t, r3)
	assert.Contains(t, r3.InitialCommand, "ulimit")

	// enfile
	r4 := SelectRecipe("enfile system limit reached")
	require.NotNil(t, r4)
	assert.Contains(t, r4.InitialCommand, "ulimit")
}

// =============================================================================
// PERMISSION SUBCATEGORY — regular vs noexec vs selinux/apparmor routing
// =============================================================================

func TestBroadPermissionSubcategory(t *testing.T) {
	// Regular permission
	r := SelectRecipe("permission denied on /etc/config")
	require.NotNil(t, r)
	assert.Equal(t, RecipePermission, r.Name)
	assert.Contains(t, r.InitialCommand, "ls -la")

	// Noexec mount
	r2 := SelectRecipe("noexec preventing script execution on /tmp")
	require.NotNil(t, r2)
	assert.Equal(t, RecipePermissionMount, r2.Name)
	assert.Contains(t, r2.InitialCommand, "noexec")

	// Script is 755 but still denied (noexec subcategory trigger)
	r3 := SelectRecipe("permission denied on script even though it's 755, still can't execute")
	require.NotNil(t, r3)
	assert.Equal(t, RecipePermissionMount, r3.Name)

	// Selinux/apparmor — still permission class
	r4 := SelectRecipe("selinux blocking access")
	require.NotNil(t, r4)
	assert.Equal(t, IssuePermission, r4.IssueClass)

	r5 := SelectRecipe("apparmor profile denying access")
	require.NotNil(t, r5)
	assert.Equal(t, IssuePermission, r5.IssueClass)
}

// =============================================================================
// DISK SUBCATEGORY — usage vs inodes
// =============================================================================

func TestBroadDiskSubcategory(t *testing.T) {
	// Regular disk usage
	r := SelectRecipe("disk space is running low")
	require.NotNil(t, r)
	assert.Equal(t, RecipeDiskUsage, r.Name)
	assert.Equal(t, "df -h", r.InitialCommand)

	// Inode exhaustion (requires compound match)
	r2 := SelectRecipe("cannot create files even though df shows space available")
	require.NotNil(t, r2)
	assert.Equal(t, RecipeDiskInodes, r2.Name)
	assert.Equal(t, "df -i", r2.InitialCommand)

	// "inode" alone without "space available" context → still regular disk
	r3 := SelectRecipe("disk inode usage")
	require.NotNil(t, r3)
	assert.Equal(t, RecipeDiskUsage, r3.Name, "inode without space-available context should default to disk_usage")

	// "no space left" + "shows space" → inodes
	r4 := SelectRecipe("no space left on device but df shows space, what's going on")
	require.NotNil(t, r4)
	assert.Equal(t, RecipeDiskInodes, r4.Name)
}

// =============================================================================
// DNS SUBCATEGORY — resolution vs hosts file
// =============================================================================

func TestBroadDNSSubcategory(t *testing.T) {
	// Regular DNS resolution
	r := SelectRecipe("can't resolve any hostnames")
	require.NotNil(t, r)
	assert.Equal(t, RecipeDNSResolution, r.Name)

	// Hosts file override
	r2 := SelectRecipe("api.internal resolves to wrong ip address")
	require.NotNil(t, r2)
	assert.Equal(t, RecipeDNSHosts, r2.Name)
	assert.Contains(t, r2.InitialCommand, "/etc/hosts")
	assert.Contains(t, r2.InitialCommand, "api.internal")

	// Wrong address without hostname — falls through to regular DNS
	r3 := SelectRecipe("resolves to wrong ip")
	require.NotNil(t, r3)
	assert.Equal(t, RecipeDNSResolution, r3.Name, "no hostname extracted should default to regular DNS")
}

// =============================================================================
// BUILD TOOL ROUTING — verify correct build commands
// =============================================================================

func TestBroadBuildToolRouting(t *testing.T) {
	tests := []struct {
		input   string
		wantSub string
		note    string
	}{
		{"npm build fails with errors", "npm run build", "npm"},
		{"yarn build broken", "yarn build", "yarn"},
		{"pnpm run build failing", "pnpm run build", "pnpm"},
		{"cargo build won't compile", "cargo build", "cargo"},
		{"go build failing", "go build", "go"},
		{"make fails with error", "make", "make"},
		{"tsc compilation errors everywhere", "tsc", "tsc"},
		// Unknown tool — should check for config files
		{"build fails but I don't know why", "ls", "unknown tool should ls for config files"},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			r := SelectRecipe(tc.input)
			require.NotNil(t, r)
			assert.Equal(t, RecipeBuildFailure, r.Name)
			assert.Contains(t, r.InitialCommand, tc.wantSub)
		})
	}
}

// =============================================================================
// hasHighDiskUsage internal function — edge cases
// =============================================================================

func TestBroadHasHighDiskUsage(t *testing.T) {
	tests := []struct {
		output string
		want   bool
		note   string
	}{
		{"Use% 84%", false, "below threshold"},
		{"Use% 85%", true, "at threshold"},
		{"Use% 100%", true, "max"},
		{"no percentages here", false, "no match"},
		{"0%", false, "zero"},
		{"text 84% more text 86%", true, "mixed - one above"},
		{"   85%   ", true, "with whitespace"},
		{"abc%", false, "non-numeric percent"},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			assert.Equal(t, tc.want, hasHighDiskUsage(tc.output))
		})
	}
}

// =============================================================================
// looksLikeKnowledgeQuery — direct tests
// =============================================================================

func TestBroadLooksLikeKnowledgeQuery(t *testing.T) {
	yes := []string{
		"what is DNS",
		"what are containers",
		"what does systemctl do",
		"explain how cgroups work",
		"define swap",
		"describe the boot process",
		"What Is A Zombie Process", // mixed case
	}

	no := []string{
		"disk is full what now",
		"my server won't start",
		"how to fix dns",          // "how to" doesn't trigger
		"check what is happening", // "what is" not at start
		"",
		"explain",                 // just the prefix, nothing after (still matches HasPrefix)
	}

	for _, q := range yes {
		assert.True(t, looksLikeKnowledgeQuery(q), "should be knowledge: %q", q)
	}

	for _, q := range no {
		if q == "explain" {
			// "explain " (with space) is the prefix, "explain" without space won't match
			assert.False(t, looksLikeKnowledgeQuery(q), "should not be knowledge: %q", q)
		} else {
			assert.False(t, looksLikeKnowledgeQuery(q), "should not be knowledge: %q", q)
		}
	}
}

// =============================================================================
// GENERAL CLASS — things that should fall through to IssueGeneral
// =============================================================================

func TestBroadGeneralFallthrough(t *testing.T) {
	inputs := []string{
		"help",
		"something weird happened",
		"my server has a problem",
		"things aren't working right",
		"I need assistance",
		"error occurred",
		"please check my system",
		"what's going on",
		"unknown issue",
		"it broke",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			got := ClassifyIssue(input)
			assert.Equal(t, IssueGeneral, got, "should fall through to general")
		})
	}
}

// =============================================================================
// DOCKER CONTAINER NAME EXTRACTION IN RECIPE
// =============================================================================

func TestBroadDockerContainerNameInRecipe(t *testing.T) {
	// With container name
	r := SelectRecipe("docker logs my-container shows errors")
	require.NotNil(t, r)
	assert.Equal(t, RecipeDockerCrash, r.Name)
	assert.Equal(t, "my-container", r.ServiceName)
	assert.Contains(t, r.InitialCommand, "my-container")

	// Without container name
	r2 := SelectRecipe("docker containers keep crashing")
	require.NotNil(t, r2)
	assert.Equal(t, RecipeDockerCrash, r2.Name)
	assert.Contains(t, r2.InitialCommand, "docker ps")

	// Follow-up with container name
	fu := r.FollowUpCommand("some status output")
	assert.Contains(t, fu, "docker logs")
	assert.Contains(t, fu, "my-container")

	// Follow-up without container name — should be empty
	fu2 := r2.FollowUpCommand("some status output")
	assert.Empty(t, fu2)
}

// =============================================================================
// FACT COLLECTOR COVERAGE — all classes have non-empty fact plans
// =============================================================================

func TestBroadFactCollectorCoverage(t *testing.T) {
	allClasses := []IssueClass{
		IssueGeneral, IssueDisk, IssueMemory, IssuePerformance, IssueDNS,
		IssueNetwork, IssueDocker, IssueBuild, IssueService, IssuePermission,
		IssuePort, IssueSSL, IssueGit, IssueCron, IssuePackage, IssueProcess,
		IssueSSH, IssueTime, IssueLog, IssueDatabase, IssueFirewall,
		IssueUser, IssueIO, IssueHardware, IssueBoot, IssueNFS,
	}

	for _, cls := range allClasses {
		t.Run(string(cls), func(t *testing.T) {
			plan := planFactsForIssue(cls)
			assert.NotEmpty(t, plan, "class %s should have fact collectors", cls)
			for _, collector := range plan {
				assert.NotEmpty(t, collector.Title, "collector title should not be empty")
				assert.NotNil(t, collector.Run, "collector Run function should not be nil")
			}
		})
	}
}

// =============================================================================
// containsAny helper — direct tests
// =============================================================================

func TestBroadContainsAny(t *testing.T) {
	assert.True(t, containsAny("hello world", "world"))
	assert.True(t, containsAny("hello world", "foo", "world"))
	assert.False(t, containsAny("hello world", "foo", "bar"))
	assert.False(t, containsAny("", "foo"))
	assert.True(t, containsAny("abcdef", "bcd"))
	assert.False(t, containsAny("hello", "Hello")) // case sensitive
}

// =============================================================================
// BuildDiagnosePrompt — integration test
// =============================================================================

func TestBroadBuildDiagnosePrompt(t *testing.T) {
	prompt := BuildDiagnosePrompt("my disk is full")
	assert.Contains(t, prompt, "my disk is full")
	assert.Contains(t, prompt, "disk")
	assert.Contains(t, prompt, "System Facts")
	assert.Contains(t, prompt, "Summary")
	assert.Contains(t, prompt, "Root Cause")
	assert.Contains(t, prompt, "Remediation")

	// Should not panic on empty input
	prompt2 := BuildDiagnosePrompt("")
	assert.Contains(t, prompt2, "general")

	// Should not panic on very long input
	long := strings.Repeat("error ", 1000)
	prompt3 := BuildDiagnosePrompt(long)
	assert.NotEmpty(t, prompt3)
}

// =============================================================================
// FORMAT — verify fact formatting
// =============================================================================

func TestBroadFormat(t *testing.T) {
	facts := &SystemFacts{
		Platform: "linux",
		Sections: []FactSection{
			{Title: "Test Section", Content: "test content"},
			{Title: "Empty Section", Content: ""},
			{Title: "Another Section", Content: "more content"},
		},
	}

	output := Format(facts)
	assert.Contains(t, output, "## Test Section")
	assert.Contains(t, output, "test content")
	assert.NotContains(t, output, "## Empty Section") // empty sections should be skipped
	assert.Contains(t, output, "## Another Section")
	assert.Contains(t, output, "Platform: linux")
}

// helper
func pct(pass, fail int) float64 {
	total := pass + fail
	if total == 0 {
		return 100.0
	}
	return float64(pass) / float64(total) * 100
}
