package diagnose

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests verify the macOS-specific recipe commands are generated correctly.
// They only run on darwin.

func TestSelectRecipe_MacOS_ServiceWithName(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only test")
	}

	r := SelectRecipe("nginx won't start")
	require.NotNil(t, r)
	assert.Equal(t, RecipeServiceFailure, r.Name)
	assert.Equal(t, "nginx", r.ServiceName)
	assert.Contains(t, r.InitialCommand, "launchctl list | grep -i")
	assert.Contains(t, r.InitialCommand, "nginx")
	assert.NotContains(t, r.InitialCommand, "systemctl")

	fu := r.FollowUpCommand("some output")
	assert.Contains(t, fu, "log show")
	assert.Contains(t, fu, "nginx")
	assert.NotContains(t, fu, "journalctl")
}

func TestSelectRecipe_MacOS_ServiceNoName(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only test")
	}

	r := SelectRecipe("service crashed")
	require.NotNil(t, r)
	assert.Equal(t, RecipeServiceFailure, r.Name)
	assert.Empty(t, r.ServiceName)
	assert.Contains(t, r.InitialCommand, "launchctl list | head -50")
	assert.NotContains(t, r.InitialCommand, "systemctl")

	// No follow-up when there's no service name
	fu := r.FollowUpCommand("some output")
	assert.Empty(t, fu)
}

func TestSelectRecipe_MacOS_Memory(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only test")
	}

	r := SelectRecipe("running out of memory")
	require.NotNil(t, r)
	assert.Equal(t, RecipeMemoryPressure, r.Name)
	assert.Equal(t, "vm_stat", r.InitialCommand)
}

func TestSelectRecipe_MacOS_DNS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only test")
	}

	r := SelectRecipe("dns resolution broken")
	require.NotNil(t, r)
	assert.Equal(t, RecipeDNSResolution, r.Name)
	assert.Equal(t, "scutil --dns", r.InitialCommand)
}
