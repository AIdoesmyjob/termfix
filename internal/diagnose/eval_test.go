package diagnose

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// CLASSIFICATION EVAL — exhaustive coverage of all 16 issue classes
// =============================================================================

func TestEvalClassification(t *testing.T) {
	type tc struct {
		input string
		want  IssueClass
	}

	tests := []struct {
		category string
		cases    []tc
	}{
		{
			category: "Permission",
			cases: []tc{
				{"permission denied on /var/log", IssuePermission},
				{"access denied writing to /tmp/foo", IssuePermission},
				{"chmod 755 not working on my script", IssuePermission},
				{"403 forbidden error from nginx", IssuePermission},
				{"can't write to file, permission denied", IssuePermission},
			},
		},
		{
			category: "Port",
			cases: []tc{
				{"address already in use port 8080", IssuePort},
				{"EADDRINUSE on port 3000", IssuePort},
				{"bind failed on port 443", IssuePort},
				{"port 5432 in use by another process", IssuePort},
				{"can't start server, port conflict on 80", IssuePort},
			},
		},
		{
			category: "SSL",
			cases: []tc{
				{"ssl certificate expired", IssueSSL},
				{"x509 cert error connecting to API", IssueSSL},
				{"tls handshake failed", IssueSSL},
				{"certificate verification failed", IssueSSL},
				{"ssl connection error to database", IssueSSL},
			},
		},
		{
			category: "Git",
			cases: []tc{
				{"merge conflict in main.go", IssueGit},
				{"git push rejected non-fast-forward", IssueGit},
				{"detached head after rebase", IssueGit},
				{"git pull fails with conflicts", IssueGit},
			},
		},
		{
			category: "Cron",
			cases: []tc{
				{"cron job not running", IssueCron},
				{"crontab entry missing", IssueCron},
				{"scheduled task failing at midnight", IssueCron},
				{"my timer unit keeps failing", IssueCron},
			},
		},
		{
			category: "Process",
			cases: []tc{
				{"zombie process consuming resources", IssueProcess},
				{"too many open files error", IssueProcess},
				{"defunct processes piling up", IssueProcess},
				{"ulimit too low for my application", IssueProcess},
			},
		},
		{
			category: "Package",
			cases: []tc{
				{"apt broken package dependency", IssuePackage},
				{"brew install fails with locked error", IssuePackage},
				{"dpkg was interrupted during install", IssuePackage},
				{"pip install giving permission errors", IssuePackage},
			},
		},
		{
			category: "Disk",
			cases: []tc{
				{"disk space full on root partition", IssueDisk},
				{"no space left on device", IssueDisk},
				{"filesystem is full", IssueDisk},
				{"running out of storage", IssueDisk},
				{"/var/log partition full", IssueDisk},
			},
		},
		{
			category: "Memory",
			cases: []tc{
				{"out of memory killed my process", IssueMemory},
				{"high memory usage on server", IssueMemory},
				{"swap is full", IssueMemory},
				{"OOM killer triggered", IssueMemory},
				{"system running low on RAM", IssueMemory},
			},
		},
		{
			category: "Performance",
			cases: []tc{
				{"server is very slow", IssuePerformance},
				{"high CPU load average", IssuePerformance},
				{"system is sluggish and unresponsive", IssuePerformance},
				{"application hanging on startup", IssuePerformance},
				{"beachball spinning on mac", IssuePerformance},
			},
		},
		{
			category: "DNS",
			cases: []tc{
				{"can't resolve hostname", IssueDNS},
				{"DNS resolution failing", IssueDNS},
				{"domain not found error", IssueDNS},
				{"resolv.conf looks wrong", IssueDNS},
			},
		},
		{
			category: "Network",
			cases: []tc{
				{"no internet connectivity", IssueNetwork},
				{"wifi keeps dropping", IssueNetwork},
				{"high latency to server", IssueNetwork},
				{"packet loss on eth0", IssueNetwork},
				{"network routing issues", IssueNetwork},
				{"machine is offline", IssueNetwork},
			},
		},
		{
			category: "Docker",
			cases: []tc{
				{"docker container crashing on start", IssueDocker},
				{"dockerfile build failing", IssueDocker},
				{"docker compose up fails", IssueDocker},
				{"container image pull fails", IssueDocker},
			},
		},
		{
			category: "Build",
			cases: []tc{
				{"npm build failing with errors", IssueBuild},
				{"go build compilation error", IssueBuild},
				{"cargo build fails with missing crate", IssueBuild},
				{"webpack bundle too large", IssueBuild},
				{"tsc typescript compilation errors", IssueBuild},
				{"make fails in project", IssueBuild},
			},
		},
		{
			category: "Service",
			cases: []tc{
				{"nginx won't start", IssueService},
				{"postgres keeps crashing", IssueService},
				{"systemd service failed to start", IssueService},
				{"redis daemon restarting in a loop", IssueService},
				{"my sshd service is down", IssueService},
			},
		},
		{
			category: "General",
			cases: []tc{
				{"general system health check", IssueGeneral},
				{"", IssueGeneral},
				{"something weird is happening", IssueGeneral},
			},
		},
	}

	totalPass, totalFail := 0, 0
	var failures []string

	for _, group := range tests {
		groupPass, groupFail := 0, 0
		for _, c := range group.cases {
			got := ClassifyIssue(c.input)
			if got == c.want {
				groupPass++
				totalPass++
			} else {
				groupFail++
				totalFail++
				failures = append(failures, fmt.Sprintf("  [%s] %q → got %s, want %s", group.category, c.input, got, c.want))
			}
		}
		t.Logf("%-12s: %d/%d passed", group.category, groupPass, groupPass+groupFail)
	}

	t.Logf("\n=== CLASSIFICATION TOTAL: %d/%d (%.0f%%) ===", totalPass, totalPass+totalFail, float64(totalPass)/float64(totalPass+totalFail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if totalFail > 0 {
		t.Errorf("%d classification failures", totalFail)
	}
}

// =============================================================================
// CLASSIFICATION EDGE CASES — ambiguous or overlapping keywords
// =============================================================================

func TestEvalClassificationEdgeCases(t *testing.T) {
	type tc struct {
		input    string
		want     IssueClass
		note     string
	}

	cases := []tc{
		// Ambiguous: "permission" + "service" — permission should win (ordered first)
		{"nginx permission denied on port 80", IssuePermission, "permission should take priority over service name"},
		// Ambiguous: "port" mentioned but in network context
		{"network port scan shows issues", IssueNetwork, "should be network not port since no conflict keywords"},
		// "slow" could be perf but "docker" is more specific
		{"docker container is slow", IssueDocker, "docker keyword should win"},
		// "build" + "docker" — docker should win (checked first in switch)
		{"docker build failing", IssueDocker, "docker should win over build"},
		// "failed to start" is service but "docker" comes first
		{"docker failed to start", IssueDocker, "docker keyword should win"},
		// "full" is disk but "memory" is more specific
		{"memory is full", IssueMemory, "memory keyword should win over 'full'"},
		// Git repo context but really a build issue
		{"npm build fails in git repo", IssueBuild, "build should win; git only matches specific git ops"},
		// SSL in a service context
		{"ssl cert expired on nginx", IssueSSL, "SSL should win over service name"},
		// Knowledge query: ClassifyIssue is keyword-based; bypass is SelectRecipe's job
		{"what is a zombie process", IssueProcess, "ClassifyIssue matches keywords; SelectRecipe handles knowledge bypass"},
		// Very short input
		{"slow", IssuePerformance, "single keyword should still match"},
		{"dns", IssueDNS, "single keyword should still match"},
		{"docker", IssueDocker, "single keyword should still match"},
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

	t.Logf("\n=== EDGE CASES: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d edge case failures", fail)
	}
}

// =============================================================================
// RECIPE SELECTION + COMMAND QUALITY EVAL
// =============================================================================

func TestEvalRecipeSelection(t *testing.T) {
	type tc struct {
		input         string
		wantRecipe    RecipeName
		wantCmd       string // substring that must appear in InitialCommand
		wantService   string // expected ServiceName (empty = don't check)
	}

	tests := []tc{
		// New recipes
		{"permission denied on /var/log/app.log", RecipePermission, "ls -la", "/var/log/app.log"},
		{"address already in use port 8080", RecipePortConflict, "", "8080"},
		{"ssl certificate expired", RecipeSSL, "openssl", ""},
		{"merge conflict in main.go", RecipeGit, "git status", ""},
		{"cron job not running", RecipeCron, "crontab", ""},
		{"apt broken package", RecipePackage, "", ""},
		{"zombie processes", RecipeProcess, "defunct", ""},
		{"too many open files", RecipeProcess, "ulimit", ""},

		// Existing recipes
		{"disk space is full", RecipeDiskUsage, "df -h", ""},
		{"server out of memory", RecipeMemoryPressure, "", ""},
		{"system very slow high load", RecipePerformanceCPU, "uptime", ""},
		{"can't resolve dns names", RecipeDNSResolution, "", ""},
		{"no internet connectivity", RecipeNetworkConnectivity, "", ""},
		{"nginx keeps crashing", RecipeServiceFailure, "", "nginx"},
		{"docker container won't start", RecipeDockerCrash, "docker", ""},
		{"npm build fails", RecipeBuildFailure, "npm", "npm"},
	}

	pass, fail := 0, 0
	var failures []string

	for _, c := range tests {
		r := SelectRecipe(c.input)
		if r == nil {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → nil recipe, want %s", c.input, c.wantRecipe))
			continue
		}
		ok := true
		var reasons []string
		if r.Name != c.wantRecipe {
			ok = false
			reasons = append(reasons, fmt.Sprintf("recipe=%s want=%s", r.Name, c.wantRecipe))
		}
		if c.wantCmd != "" && !strings.Contains(r.InitialCommand, c.wantCmd) {
			ok = false
			reasons = append(reasons, fmt.Sprintf("cmd=%q missing %q", r.InitialCommand, c.wantCmd))
		}
		if c.wantService != "" && r.ServiceName != c.wantService {
			ok = false
			reasons = append(reasons, fmt.Sprintf("service=%q want=%q", r.ServiceName, c.wantService))
		}
		if ok {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → %s", c.input, strings.Join(reasons, "; ")))
		}
	}

	t.Logf("\n=== RECIPE SELECTION: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d recipe selection failures", fail)
	}
}

// =============================================================================
// FOLLOW-UP COMMAND QUALITY EVAL
// =============================================================================

func TestEvalFollowUpCommands(t *testing.T) {
	type tc struct {
		input       string
		firstOutput string
		wantSubstr  string // if empty, expect empty follow-up
		wantEmpty   bool
	}

	isDarwin := runtime.GOOS == "darwin"

	tests := []tc{
		// Disk: high usage should trigger du
		{"disk full", "Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 100G 95G 5G 95% /", "du", false},
		// Disk: low usage should NOT trigger follow-up
		{"disk usage", "Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 100G 40G 60G 40% /", "", true},
		// Git: always has follow-up
		{"merge conflict", "some output", "git log", false},
		// SSL: always has follow-up
		{"ssl cert expired", "some output", "date", false},
		// Permission: with path extracted
		{"permission denied on /var/log/app.log", "some output", "stat", false},
		// Port: with port extracted
		{"address already in use port 8080", "some output", "8080", false},
		// Cron: should have log follow-up
		{"cron not working", "some output", "", false},
		// Process: should have lsof follow-up
		{"zombie process", "some output", "lsof", false},
		// Package: no follow-up
		{"apt broken", "some output", "", true},
		// Service with name: should have log follow-up
		{"nginx crashing", "nginx output here", "", false},
	}

	pass, fail := 0, 0
	var failures []string
	_ = isDarwin

	for _, c := range tests {
		r := SelectRecipe(c.input)
		if r == nil {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → nil recipe", c.input))
			continue
		}
		followUp := r.FollowUpCommand(c.firstOutput)
		ok := true
		if c.wantEmpty && followUp != "" {
			ok = false
			failures = append(failures, fmt.Sprintf("  %q → got follow-up %q, expected empty", c.input, followUp))
		} else if !c.wantEmpty && c.wantSubstr != "" && !strings.Contains(followUp, c.wantSubstr) {
			ok = false
			failures = append(failures, fmt.Sprintf("  %q → follow-up %q missing %q", c.input, followUp, c.wantSubstr))
		} else if !c.wantEmpty && followUp == "" {
			ok = false
			failures = append(failures, fmt.Sprintf("  %q → expected follow-up but got empty", c.input))
		}
		if ok {
			pass++
		} else {
			fail++
		}
	}

	t.Logf("\n=== FOLLOW-UP COMMANDS: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d follow-up command failures", fail)
	}
}

// =============================================================================
// KNOWLEDGE QUERY BYPASS EVAL
// =============================================================================

func TestEvalKnowledgeBypass(t *testing.T) {
	// These should return nil from SelectRecipe (bypass to model knowledge)
	knowledgeQueries := []string{
		"what is SSH",
		"what are environment variables",
		"what does systemctl do",
		"explain how DNS works",
		"define TCP three way handshake",
		"describe what a zombie process is",
	}

	// These should NOT be treated as knowledge queries
	actionQueries := []string{
		"disk full what should I do",
		"why is my port 8080 in use",
		"check memory usage",
		"nginx won't start",
		"ssl cert expired",
	}

	pass, fail := 0, 0

	for _, q := range knowledgeQueries {
		r := SelectRecipe(q)
		if r == nil {
			pass++
		} else {
			fail++
			t.Logf("  FAIL: knowledge query %q got recipe %s (should be nil)", q, r.Name)
		}
	}

	for _, q := range actionQueries {
		r := SelectRecipe(q)
		if r != nil {
			pass++
		} else {
			fail++
			t.Logf("  FAIL: action query %q got nil recipe (should have recipe)", q)
		}
	}

	t.Logf("\n=== KNOWLEDGE BYPASS: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	if fail > 0 {
		t.Errorf("%d knowledge bypass failures", fail)
	}
}

// =============================================================================
// EXTRACTION HELPERS EVAL
// =============================================================================

func TestEvalExtractors(t *testing.T) {
	pass, fail := 0, 0
	var failures []string

	// Port extraction
	portCases := []struct {
		input string
		want  string
	}{
		{"port 8080 in use", "8080"},
		{"EADDRINUSE port 3000", "3000"},
		{"address already in use 443", "443"},
		{"bind to port 5432 failed", "5432"},
		{"no port here", ""},
		{"port 80 is busy", "80"},
		{"listening on 9090", "9090"},
	}
	for _, c := range portCases {
		got := ExtractPort(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractPort(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	// Path extraction
	pathCases := []struct {
		input string
		want  string
	}{
		{"permission denied on /var/log/app.log", "/var/log/app.log"},
		{"can't read /etc/nginx/nginx.conf", "/etc/nginx/nginx.conf"},
		{"error at /home/user/.config/app", "/home/user/.config/app"},
		{"no path here", ""},
	}
	for _, c := range pathCases {
		got := ExtractPath(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractPath(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	// Service name extraction
	serviceCases := []struct {
		input string
		want  string
	}{
		{"nginx keeps crashing", "nginx"},
		{"postgres won't start", "postgres"},
		{"redis service is down", "redis"},
		{"my app service failed", "app"},
		{"sshd not responding", "sshd"},
	}
	for _, c := range serviceCases {
		got := ExtractServiceName(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractServiceName(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	// Container name extraction
	containerCases := []struct {
		input string
		want  string
	}{
		{"docker logs my-container", "my-container"},
		{"container restart web-app", "web-app"},  // "restart" is a valid docker verb in regex
		{"docker inspect redis-cache", "redis-cache"},
	}
	for _, c := range containerCases {
		got := ExtractContainerName(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractContainerName(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	// Build tool extraction
	buildCases := []struct {
		input string
		want  string
	}{
		{"npm run build fails", "npm"},
		{"cargo build error", "cargo"},
		{"go build ./... fails", "go"},
		{"webpack bundle size too large", "webpack"},
		{"tsc compilation errors", "tsc"},
	}
	for _, c := range buildCases {
		got := ExtractBuildTool(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractBuildTool(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	t.Logf("\n=== EXTRACTORS: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d extractor failures", fail)
	}
}

// =============================================================================
// NEW ISSUE CLASS CLASSIFICATION
// =============================================================================

func TestEvalNewIssueClasses(t *testing.T) {
	type tc struct {
		input string
		want  IssueClass
	}

	tests := []struct {
		category string
		cases    []tc
	}{
		{
			category: "SSH",
			cases: []tc{
				{"ssh publickey authentication failing", IssueSSH},
				{"host key verification failed", IssueSSH},
				{"authorized_keys not working", IssueSSH},
				{"ssh connection refused to server", IssueSSH},
				{"ssh timeout connecting to host", IssueSSH},
			},
		},
		{
			category: "Time",
			cases: []tc{
				{"ntp not syncing", IssueTime},
				{"clock skew detected", IssueTime},
				{"chrony is not running", IssueTime},
				{"timezone is wrong", IssueTime},
				{"time drift on server", IssueTime},
			},
		},
		{
			category: "Log",
			cases: []tc{
				{"journald taking too much space", IssueLog},
				{"syslog is huge", IssueLog},
				{"logrotate not rotating", IssueLog},
				{"logs too big on server", IssueLog},
				{"/var/log growing out of control", IssueLog},
			},
		},
		{
			category: "Database",
			cases: []tc{
				{"postgres database connection refused", IssueDatabase},
				{"mysql database slow query", IssueDatabase},
				{"database connection timeout", IssueDatabase},
				{"redis connection refused", IssueDatabase},
			},
		},
		{
			category: "Firewall",
			cases: []tc{
				{"firewall blocking traffic", IssueFirewall},
				{"iptables rules wrong", IssueFirewall},
				{"ufw blocking connections", IssueFirewall},
				{"nftables config issue", IssueFirewall},
				{"port blocked by firewall", IssueFirewall},
			},
		},
		{
			category: "User",
			cases: []tc{
				{"locked account for admin", IssueUser},
				{"login failed for user", IssueUser},
				{"password expired on server", IssueUser},
				{"user account locked out", IssueUser},
				{"wrong shell for user", IssueUser},
				{"nologin shell set", IssueUser},
			},
		},
		{
			category: "IO",
			cases: []tc{
				{"high io wait on server", IssueIO},
				{"iowait at 90%", IssueIO},
				{"disk slow writes", IssueIO},
				{"iostat shows bottleneck", IssueIO},
				{"disk latency is high", IssueIO},
			},
		},
		{
			category: "Hardware",
			cases: []tc{
				{"smart disk error detected", IssueHardware},
				{"bad sector on drive", IssueHardware},
				{"temperature too high", IssueHardware},
				{"thermal throttling cpu", IssueHardware},
				{"mce errors in dmesg", IssueHardware},
				{"fan not spinning", IssueHardware},
			},
		},
		{
			category: "Boot",
			cases: []tc{
				{"server won't boot", IssueBoot},
				{"grub error on startup", IssueBoot},
				{"fstab misconfigured", IssueBoot},
				{"kernel panic on boot", IssueBoot},
				{"initramfs error", IssueBoot},
				{"startup fails with error", IssueBoot},
			},
		},
		{
			category: "NFS",
			cases: []tc{
				{"nfs mount not working", IssueNFS},
				{"stale file handle error", IssueNFS},
				{"showmount returns nothing", IssueNFS},
				{"mount failed with timeout", IssueNFS},
				{"rpc error on mount", IssueNFS},
			},
		},
	}

	totalPass, totalFail := 0, 0
	var failures []string

	for _, group := range tests {
		groupPass, groupFail := 0, 0
		for _, c := range group.cases {
			got := ClassifyIssue(c.input)
			if got == c.want {
				groupPass++
				totalPass++
			} else {
				groupFail++
				totalFail++
				failures = append(failures, fmt.Sprintf("  [%s] %q → got %s, want %s", group.category, c.input, got, c.want))
			}
		}
		t.Logf("%-12s: %d/%d passed", group.category, groupPass, groupPass+groupFail)
	}

	t.Logf("\n=== NEW CLASSES: %d/%d (%.0f%%) ===", totalPass, totalPass+totalFail, float64(totalPass)/float64(totalPass+totalFail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if totalFail > 0 {
		t.Errorf("%d new class classification failures", totalFail)
	}
}

// =============================================================================
// SUBCATEGORY ROUTING TESTS
// =============================================================================

func TestEvalSubcategoryRouting(t *testing.T) {
	t.Run("disk inodes vs disk usage", func(t *testing.T) {
		r := SelectRecipe("can't create files, df shows space available")
		require.NotNil(t, r)
		assert.Equal(t, RecipeDiskInodes, r.Name)
		assert.Equal(t, "df -i", r.InitialCommand)

		r2 := SelectRecipe("disk is almost full")
		require.NotNil(t, r2)
		assert.Equal(t, RecipeDiskUsage, r2.Name)
		assert.Equal(t, "df -h", r2.InitialCommand)
	})

	t.Run("permission mount noexec vs regular permission", func(t *testing.T) {
		r := SelectRecipe("noexec mount preventing script execution")
		require.NotNil(t, r)
		assert.Equal(t, RecipePermissionMount, r.Name)
		assert.Contains(t, r.InitialCommand, "noexec")

		r2 := SelectRecipe("permission denied on /var/log/app.log")
		require.NotNil(t, r2)
		assert.Equal(t, RecipePermission, r2.Name)
		assert.Contains(t, r2.InitialCommand, "ls -la")
	})

	t.Run("dns hosts vs dns resolution", func(t *testing.T) {
		r := SelectRecipe("api.internal resolves to wrong ip")
		require.NotNil(t, r)
		assert.Equal(t, RecipeDNSHosts, r.Name)
		assert.Contains(t, r.InitialCommand, "/etc/hosts")

		r2 := SelectRecipe("dns not resolving anything")
		require.NotNil(t, r2)
		assert.Equal(t, RecipeDNSResolution, r2.Name)
	})
}

// =============================================================================
// NEW RECIPE SELECTION TESTS
// =============================================================================

func TestEvalNewRecipeSelection(t *testing.T) {
	type tc struct {
		input      string
		wantRecipe RecipeName
		wantCmd    string
	}

	tests := []tc{
		{"ssh publickey authentication failing", RecipeSSH, "ssh-add"},
		{"ntp not syncing", RecipeTime, ""},
		{"journald taking too much space", RecipeLog, ""},
		{"postgres database connection refused", RecipeDatabase, "pg_isready"},
		{"firewall blocking traffic", RecipeFirewall, ""},
		{"io wait too high", RecipeIO, "iostat"},
		{"temperature too high on server", RecipeHardware, ""},
		{"server won't boot after update", RecipeBoot, ""},
		{"nfs mount is stuck", RecipeNFS, "mount"},
		{"locked account for admin user", RecipeUser, ""},
	}

	pass, fail := 0, 0
	var failures []string

	for _, c := range tests {
		r := SelectRecipe(c.input)
		if r == nil {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → nil recipe, want %s", c.input, c.wantRecipe))
			continue
		}
		ok := true
		var reasons []string
		if r.Name != c.wantRecipe {
			ok = false
			reasons = append(reasons, fmt.Sprintf("recipe=%s want=%s", r.Name, c.wantRecipe))
		}
		if c.wantCmd != "" && !strings.Contains(r.InitialCommand, c.wantCmd) {
			ok = false
			reasons = append(reasons, fmt.Sprintf("cmd=%q missing %q", r.InitialCommand, c.wantCmd))
		}
		if ok {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → %s", c.input, strings.Join(reasons, "; ")))
		}
	}

	t.Logf("\n=== NEW RECIPE SELECTION: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d new recipe selection failures", fail)
	}
}

// =============================================================================
// NEW EXTRACTOR TESTS
// =============================================================================

func TestEvalNewExtractors(t *testing.T) {
	pass, fail := 0, 0
	var failures []string

	// Hostname extraction
	hostCases := []struct {
		input string
		want  string
	}{
		{"api.internal resolves to wrong ip", "api.internal"},
		{"myapp.local is unreachable", "myapp.local"},
		{"db.prod.example.com is down", "db.prod.example.com"},
		{"single word hostname", ""},
	}
	for _, c := range hostCases {
		got := ExtractHostname(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractHostname(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	// Database type extraction
	dbCases := []struct {
		input string
		want  string
	}{
		{"postgres connection refused", "postgres"},
		{"mysql is not starting", "mysql"},
		{"redis connection timeout", "redis"},
		{"mongodb cluster down", "mongodb"},
		{"elasticsearch slow", "elasticsearch"},
		{"no database mentioned", ""},
	}
	for _, c := range dbCases {
		got := ExtractDatabaseType(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractDatabaseType(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	// Username extraction
	userCases := []struct {
		input string
		want  string
	}{
		{"user john cannot login", "john"},
		{"account admin is locked", "admin"},
		{"login deploy failed", "deploy"},
		{"no user mentioned here", ""},
	}
	for _, c := range userCases {
		got := ExtractUsername(c.input)
		if got == c.want {
			pass++
		} else {
			fail++
			failures = append(failures, fmt.Sprintf("  ExtractUsername(%q) = %q, want %q", c.input, got, c.want))
		}
	}

	t.Logf("\n=== NEW EXTRACTORS: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d new extractor failures", fail)
	}
}

// =============================================================================
// NEW FOLLOW-UP COMMAND TESTS
// =============================================================================

func TestEvalNewFollowUpCommands(t *testing.T) {
	type tc struct {
		input       string
		firstOutput string
		wantSubstr  string
		wantEmpty   bool
	}

	tests := []tc{
		{"ssh publickey authentication failing", "ls output", "config", false},
		{"ntp not syncing", "timedatectl output", "", false},
		{"journald too big", "disk usage output", "", false},
		{"postgres database connection refused", "pg_isready output", "journalctl", false},
		{"firewall blocking", "iptables output", "", false},
		{"nfs mount stuck", "mount output", "showmount", false},
		{"server won't boot", "journal output", "fstab", false},
		{"disk inodes problem, can't create files, df shows space available", "df -i output", "find", false},
	}

	pass, fail := 0, 0
	var failures []string

	for _, c := range tests {
		r := SelectRecipe(c.input)
		if r == nil {
			fail++
			failures = append(failures, fmt.Sprintf("  %q → nil recipe", c.input))
			continue
		}
		followUp := r.FollowUpCommand(c.firstOutput)
		ok := true
		if c.wantEmpty && followUp != "" {
			ok = false
			failures = append(failures, fmt.Sprintf("  %q → got follow-up %q, expected empty", c.input, followUp))
		} else if !c.wantEmpty && c.wantSubstr != "" && !strings.Contains(followUp, c.wantSubstr) {
			ok = false
			failures = append(failures, fmt.Sprintf("  %q → follow-up %q missing %q", c.input, followUp, c.wantSubstr))
		} else if !c.wantEmpty && followUp == "" {
			ok = false
			failures = append(failures, fmt.Sprintf("  %q → expected follow-up but got empty", c.input))
		}
		if ok {
			pass++
		} else {
			fail++
		}
	}

	t.Logf("\n=== NEW FOLLOW-UP COMMANDS: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d new follow-up command failures", fail)
	}
}

// =============================================================================
// CROSS-CLASS PRIORITY / EDGE CASES
// =============================================================================

func TestEvalNewEdgeCases(t *testing.T) {
	type tc struct {
		input string
		want  IssueClass
		note  string
	}

	cases := []tc{
		// SSH service name should still route to service (not SSH) unless SSH-specific context
		{"sshd service is down", IssueService, "sshd as service name should route to service"},
		// postgres without DB context should be service
		{"postgres keeps crashing", IssueService, "postgres without DB context should be service"},
		// postgres with DB context should be database
		{"postgres database connection refused", IssueDatabase, "postgres with DB context should be database"},
		// /var/log in permission context should stay permission
		{"/var/log partition full", IssueDisk, "/var/log in disk context should be disk"},
		// boot vs disk: fstab is boot-specific
		{"fstab entry wrong causing mount failure", IssueBoot, "fstab should route to boot"},
		// firewall vs network
		{"iptables dropping packets", IssueFirewall, "iptables should route to firewall not network"},
		// NFS vs mount
		{"nfs stale file handle", IssueNFS, "NFS-specific mount issue"},
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

	t.Logf("\n=== NEW EDGE CASES: %d/%d (%.0f%%) ===", pass, pass+fail, float64(pass)/float64(pass+fail)*100)
	for _, f := range failures {
		t.Log(f)
	}
	if fail > 0 {
		t.Errorf("%d new edge case failures", fail)
	}
}

// =============================================================================
// FACT COLLECTOR COVERAGE
// =============================================================================

func TestEvalNewFactCollectors(t *testing.T) {
	newClasses := []IssueClass{
		IssueSSH, IssueTime, IssueLog, IssueDatabase,
		IssueFirewall, IssueUser, IssueIO, IssueHardware,
		IssueBoot, IssueNFS,
	}

	for _, cls := range newClasses {
		t.Run(string(cls), func(t *testing.T) {
			plan := planFactsForIssue(cls)
			assert.NotEmpty(t, plan, "planFactsForIssue(%s) should not be empty", cls)
		})
	}
}
