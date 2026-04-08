package fileutil

import (
	"testing"
)

func TestSkipHidden(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		// Hidden files (dot-prefixed)
		{name: "hidden file", path: ".gitignore", want: true},
		// Note: SkipHidden only checks filepath.Base for dot prefix, so
		// a dot-prefixed parent dir that is NOT in commonIgnoredDirs is not hidden.
		{name: "dot-prefixed parent not in ignored list", path: ".config/file.txt", want: false},
		{name: "hidden nested file", path: "src/.hidden", want: true},
		{name: "dot-dot prefix is not hidden", path: "..something", want: true},

		// Current directory dot is not hidden
		{name: "current directory dot", path: ".", want: false},

		// Common ignored directories
		{name: "node_modules", path: "node_modules/package.json", want: true},
		{name: "vendor", path: "vendor/lib/main.go", want: true},
		{name: "dist", path: "dist/bundle.js", want: true},
		{name: "build", path: "build/output.js", want: true},
		{name: "target", path: "target/classes/Main.class", want: true},
		{name: ".git directory", path: ".git/config", want: true},
		{name: ".idea directory", path: ".idea/workspace.xml", want: true},
		{name: ".vscode directory", path: ".vscode/settings.json", want: true},
		{name: "__pycache__", path: "__pycache__/module.pyc", want: true},
		{name: "bin", path: "bin/app", want: true},
		{name: "obj", path: "obj/Debug/net6.0/app.dll", want: true},
		{name: "out", path: "out/production/Main.class", want: true},
		{name: "coverage", path: "coverage/lcov.info", want: true},
		{name: "tmp", path: "tmp/tempfile", want: true},
		{name: "temp", path: "temp/data", want: true},
		{name: "logs", path: "logs/app.log", want: true},
		{name: "generated", path: "generated/types.go", want: true},
		{name: "bower_components", path: "bower_components/jquery/jquery.js", want: true},
		{name: "jspm_packages", path: "jspm_packages/npm/lodash.js", want: true},
		{name: ".termfix", path: ".termfix/config.json", want: true},

		// Nested ignored directories
		{name: "nested node_modules", path: "project/node_modules/pkg/index.js", want: true},
		{name: "nested vendor", path: "app/vendor/autoload.php", want: true},
		{name: "deeply nested .git", path: "a/b/.git/HEAD", want: true},

		// Non-hidden, non-ignored paths
		{name: "regular file", path: "main.go", want: false},
		{name: "regular nested file", path: "src/app/main.go", want: false},
		{name: "file in regular dir", path: "cmd/server/main.go", want: false},
		{name: "internal directory", path: "internal/pkg/util.go", want: false},
		{name: "lib directory", path: "lib/helper.go", want: false},
		{name: "src directory", path: "src/index.ts", want: false},

		// Edge cases
		{name: "empty string", path: "", want: false},
		{name: "just a slash", path: "/", want: false},
		{name: "file named build but not dir", path: "rebuild/file.go", want: false},
		{name: "file named vendor-extra", path: "vendor-extra/file.go", want: false},
		{name: "substring match should not trigger", path: "nonode_modules/file.js", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SkipHidden(tt.path)
			if got != tt.want {
				t.Errorf("SkipHidden(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
