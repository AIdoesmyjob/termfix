package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffError(t *testing.T) {
	err := NewDiffError("something went wrong")
	assert.Equal(t, "something went wrong", err.Error())
}

func TestIdentifyFilesNeeded(t *testing.T) {
	patch := "*** Update File: foo.go\n*** Delete File: bar.go\n*** End Patch"
	files := IdentifyFilesNeeded(patch)
	assert.ElementsMatch(t, []string{"foo.go", "bar.go"}, files)
}

func TestIdentifyFilesAdded(t *testing.T) {
	patch := "*** Add File: new.go\n*** End Patch"
	files := IdentifyFilesAdded(patch)
	assert.Equal(t, []string{"new.go"}, files)
}

func TestIdentifyFilesEmpty(t *testing.T) {
	patch := "*** End Patch"
	needed := IdentifyFilesNeeded(patch)
	added := IdentifyFilesAdded(patch)
	assert.Empty(t, needed)
	assert.Empty(t, added)
}

func TestAssembleChangesUpdate(t *testing.T) {
	orig := map[string]string{"a.go": "old"}
	updated := map[string]string{"a.go": "new"}
	commit := AssembleChanges(orig, updated)
	require.Len(t, commit.Changes, 1)
	change, ok := commit.Changes["a.go"]
	require.True(t, ok, "expected change for a.go")
	assert.Equal(t, ActionUpdate, change.Type)
}

func TestAssembleChangesAdd(t *testing.T) {
	orig := map[string]string{}
	updated := map[string]string{"new.go": "content"}
	commit := AssembleChanges(orig, updated)
	require.Len(t, commit.Changes, 1)
	change, ok := commit.Changes["new.go"]
	require.True(t, ok, "expected change for new.go")
	assert.Equal(t, ActionAdd, change.Type)
}

func TestAssembleChangesDelete(t *testing.T) {
	orig := map[string]string{"old.go": "content"}
	updated := map[string]string{"old.go": ""}
	commit := AssembleChanges(orig, updated)
	require.Len(t, commit.Changes, 1)
	change, ok := commit.Changes["old.go"]
	require.True(t, ok, "expected change for old.go")
	assert.Equal(t, ActionDelete, change.Type)
}

func TestAssembleChangesNoChange(t *testing.T) {
	orig := map[string]string{"a.go": "same"}
	updated := map[string]string{"a.go": "same"}
	commit := AssembleChanges(orig, updated)
	assert.Empty(t, commit.Changes)
}

func TestTextToPatchInvalid(t *testing.T) {
	_, _, err := TextToPatch("not a valid patch", map[string]string{})
	assert.Error(t, err)
}

func TestTextToPatchValid(t *testing.T) {
	patchText := "*** Begin Patch\n*** Add File: test.txt\n+hello world\n*** End Patch"
	patch, _, err := TextToPatch(patchText, map[string]string{})
	require.NoError(t, err)
	require.Len(t, patch.Actions, 1)
	action, ok := patch.Actions["test.txt"]
	require.True(t, ok, "expected action for test.txt")
	assert.Equal(t, ActionAdd, action.Type)
}

func TestValidatePatchValid(t *testing.T) {
	patchText := "*** Begin Patch\n*** Add File: test.txt\n+hello world\n*** End Patch"
	valid, _, err := ValidatePatch(patchText, map[string]string{})
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestValidatePatchInvalidPrefix(t *testing.T) {
	patchText := "this is not a patch"
	valid, _, err := ValidatePatch(patchText, map[string]string{})
	// Depending on implementation, this may return an error or just false
	if err != nil {
		assert.False(t, valid)
	} else {
		assert.False(t, valid)
	}
}

func TestValidatePatchMissingFile(t *testing.T) {
	patchText := "*** Begin Patch\n*** Update File: nonexistent.go\n--- a/nonexistent.go\n+++ b/nonexistent.go\n*** End Patch"
	valid, _, _ := ValidatePatch(patchText, map[string]string{})
	assert.False(t, valid)
}
