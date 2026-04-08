package completions

import (
	"testing"
)

func TestProcessNullTerminatedOutput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []string
	}{
		{
			name:  "empty input",
			input: []byte{},
			want:  []string{},
		},
		{
			name:  "nil input",
			input: nil,
			want:  []string{},
		},
		{
			name:  "single file with trailing null",
			input: []byte("main.go\x00"),
			want:  []string{"main.go"},
		},
		{
			name:  "single file without trailing null",
			input: []byte("main.go"),
			want:  []string{"main.go"},
		},
		{
			name:  "multiple files with trailing null",
			input: []byte("main.go\x00lib.go\x00util.go\x00"),
			want:  []string{"main.go", "lib.go", "util.go"},
		},
		{
			name:  "multiple files without trailing null",
			input: []byte("main.go\x00lib.go\x00util.go"),
			want:  []string{"main.go", "lib.go", "util.go"},
		},
		{
			name:  "only a null byte",
			input: []byte{0},
			want:  []string{},
		},
		{
			name:  "consecutive null bytes",
			input: []byte("a.go\x00\x00b.go\x00"),
			want:  []string{"a.go", "b.go"},
		},
		{
			name:  "hidden file is filtered",
			input: []byte(".hidden\x00visible.go\x00"),
			want:  []string{"visible.go"},
		},
		{
			name:  "node_modules path is filtered",
			input: []byte("node_modules/pkg/index.js\x00src/app.js\x00"),
			want:  []string{"src/app.js"},
		},
		{
			name:  ".git path is filtered",
			input: []byte(".git/config\x00README.md\x00"),
			want:  []string{"README.md"},
		},
		{
			name:  "all hidden entries filtered returns empty",
			input: []byte(".hidden\x00.gitignore\x00node_modules/x\x00"),
			want:  []string{},
		},
		{
			name:  "paths with subdirectories",
			input: []byte("src/main.go\x00pkg/util.go\x00"),
			want:  []string{"src/main.go", "pkg/util.go"},
		},
		{
			name:  "vendor path is filtered",
			input: []byte("vendor/lib.go\x00app/main.go\x00"),
			want:  []string{"app/main.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := processNullTerminatedOutput(tt.input)

			if len(got) != len(tt.want) {
				t.Fatalf("processNullTerminatedOutput(%q) returned %d items, want %d\ngot:  %v\nwant: %v",
					tt.input, len(got), len(tt.want), got, tt.want)
			}

			for i := range got {
				// processNullTerminatedOutput joins paths with filepath.Join(".", path),
				// so we need to account for the "./" prefix or OS path separator
				// The function uses filepath.Join(".", path) which on Unix produces "./path"
				// but filepath.Join normalizes, so "." + "main.go" = "main.go"
				// Actually filepath.Join(".", "main.go") = "main.go" on unix.
				// And filepath.Join(".", "src/main.go") = "src/main.go"
				wantPath := tt.want[i]
				if got[i] != wantPath {
					t.Errorf("item[%d] = %q, want %q", i, got[i], wantPath)
				}
			}
		})
	}
}
