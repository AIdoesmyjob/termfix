package diagnose

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIssueClassifications(t *testing.T) {
	cases := []struct {
		input string
		want  IssueClass
	}{
		{"permission denied on /var/log", IssuePermission},
		{"access denied writing to /tmp/foo", IssuePermission},
		{"chmod 755 not working", IssuePermission},
		{"403 forbidden error", IssuePermission},

		{"address already in use port 8080", IssuePort},
		{"EADDRINUSE on port 3000", IssuePort},
		{"bind failed port 443", IssuePort},

		{"ssl certificate expired", IssueSSL},
		{"x509 cert error", IssueSSL},
		{"tls handshake failed", IssueSSL},

		{"merge conflict in main.go", IssueGit},
		{"git push rejected", IssueGit},
		{"detached head after rebase", IssueGit},

		{"cron job not running", IssueCron},
		{"crontab entry missing", IssueCron},
		{"scheduled task failing", IssueCron},

		{"zombie process consuming resources", IssueProcess},
		{"too many open files", IssueProcess},
		{"defunct processes piling up", IssueProcess},

		{"apt broken package dependency", IssuePackage},
		{"brew install fails with locked", IssuePackage},
		{"dpkg was interrupted", IssuePackage},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ClassifyIssue(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewRecipeSelection(t *testing.T) {
	t.Run("permission recipe", func(t *testing.T) {
		r := SelectRecipe("permission denied on /var/log/app.log")
		require.NotNil(t, r)
		assert.Equal(t, RecipePermission, r.Name)
		assert.Contains(t, r.InitialCommand, "ls -la")
	})

	t.Run("port conflict recipe", func(t *testing.T) {
		r := SelectRecipe("address already in use port 8080")
		require.NotNil(t, r)
		assert.Equal(t, RecipePortConflict, r.Name)
		assert.Equal(t, "8080", r.ServiceName)
	})

	t.Run("ssl recipe", func(t *testing.T) {
		r := SelectRecipe("ssl certificate expired")
		require.NotNil(t, r)
		assert.Equal(t, RecipeSSL, r.Name)
		assert.Contains(t, r.InitialCommand, "openssl")
	})

	t.Run("git recipe", func(t *testing.T) {
		r := SelectRecipe("merge conflict in main.go")
		require.NotNil(t, r)
		assert.Equal(t, RecipeGit, r.Name)
		assert.Equal(t, "git status", r.InitialCommand)
		assert.Equal(t, "git log --oneline -5", r.FollowUpCommand("some output"))
	})

	t.Run("cron recipe", func(t *testing.T) {
		r := SelectRecipe("cron job not running")
		require.NotNil(t, r)
		assert.Equal(t, RecipeCron, r.Name)
		assert.Contains(t, r.InitialCommand, "crontab")
	})

	t.Run("package recipe", func(t *testing.T) {
		r := SelectRecipe("apt broken package dependency")
		require.NotNil(t, r)
		assert.Equal(t, RecipePackage, r.Name)
	})

	t.Run("process recipe zombies", func(t *testing.T) {
		r := SelectRecipe("zombie processes piling up")
		require.NotNil(t, r)
		assert.Equal(t, RecipeProcess, r.Name)
		assert.Contains(t, r.InitialCommand, "defunct")
	})

	t.Run("process recipe ulimit", func(t *testing.T) {
		r := SelectRecipe("too many open files ulimit")
		require.NotNil(t, r)
		assert.Equal(t, RecipeProcess, r.Name)
		assert.Contains(t, r.InitialCommand, "ulimit")
	})

	t.Run("knowledge query still bypasses", func(t *testing.T) {
		r := SelectRecipe("what is SSL")
		assert.Nil(t, r)
	})
}

func TestExtractPort(t *testing.T) {
	assert.Equal(t, "8080", ExtractPort("port 8080 in use"))
	assert.Equal(t, "3000", ExtractPort("EADDRINUSE on port 3000"))
	assert.Equal(t, "", ExtractPort("no port mentioned"))
}

func TestExtractPath(t *testing.T) {
	assert.Equal(t, "/var/log/app.log", ExtractPath("permission denied on /var/log/app.log"))
	assert.Equal(t, "/etc/nginx/nginx.conf", ExtractPath("cannot read /etc/nginx/nginx.conf"))
}

func TestExistingClassificationsUnchanged(t *testing.T) {
	// Ensure the new classes don't break existing ones
	cases := []struct {
		input string
		want  IssueClass
	}{
		{"disk space full", IssueDisk},
		{"out of memory", IssueMemory},
		{"cpu is very slow", IssuePerformance},
		{"dns resolution failing", IssueDNS},
		{"network connectivity lost", IssueNetwork},
		{"docker container crashing", IssueDocker},
		{"npm build failing", IssueBuild},
		{"nginx won't start", IssueService},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ClassifyIssue(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
