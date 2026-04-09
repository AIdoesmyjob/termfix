package diagnose

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectRecipe(t *testing.T) {
	recipe := SelectRecipe("disk space is almost full")
	require.NotNil(t, recipe)
	assert.Equal(t, RecipeDiskUsage, recipe.Name)
	assert.Equal(t, "df -h", recipe.InitialCommand)

	recipe = SelectRecipe("nginx won't start")
	require.NotNil(t, recipe)
	assert.Equal(t, RecipeServiceFailure, recipe.Name)
	assert.Equal(t, "nginx", recipe.ServiceName)

	recipe = SelectRecipe("what is DNS")
	assert.Nil(t, recipe)
}

func TestRecipeFollowUpCommand(t *testing.T) {
	diskRecipe := SelectRecipe("disk is full")
	require.NotNil(t, diskRecipe)
	assert.NotEmpty(t, diskRecipe.FollowUpCommand("Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 100G 90G 10G 90% /"))
	assert.Empty(t, diskRecipe.FollowUpCommand("Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 100G 40G 60G 40% /"))

	perfRecipe := SelectRecipe("system is very slow")
	require.NotNil(t, perfRecipe)
	if runtime.GOOS == "darwin" {
		assert.Equal(t, "ps -eo pid,pcpu,comm -r | head -10", perfRecipe.FollowUpCommand("load averages: 8.0 7.0 6.0"))
	} else {
		assert.Equal(t, "ps -eo pid,pcpu,comm --sort=-pcpu | head -10", perfRecipe.FollowUpCommand("load average: 8.0, 7.0, 6.0"))
	}

	serviceRecipe := SelectRecipe("postgres keeps crashing")
	require.NotNil(t, serviceRecipe)
	followUp := serviceRecipe.FollowUpCommand("postgres output")
	if runtime.GOOS == "darwin" {
		assert.Contains(t, followUp, "log show")
		assert.Contains(t, followUp, "postgres")
	} else {
		assert.Contains(t, followUp, "journalctl -u")
		assert.Contains(t, followUp, "postgres")
	}
}
