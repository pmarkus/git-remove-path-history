package main

import "testing"

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		// Exact file match
		{
			name:    "exact file match",
			pattern: "src/config.json",
			path:    "src/config.json",
			want:    true,
		},
		{
			name:    "exact file no match — different name",
			pattern: "src/config.json",
			path:    "src/config.yaml",
			want:    false,
		},

		// Directory prefix
		{
			name:    "directory prefix — immediate child file",
			pattern: "src/generated",
			path:    "src/generated/foo.go",
			want:    true,
		},
		{
			name:    "directory prefix — nested child file",
			pattern: "src/generated",
			path:    "src/generated/sub/bar.go",
			want:    true,
		},
		{
			name:    "directory prefix — sibling directory not matched",
			pattern: "src/generated",
			path:    "src/generatedOther/foo.go",
			want:    false,
		},
		{
			name:    "directory prefix — pattern with trailing slash",
			pattern: "src/generated/",
			path:    "src/generated/foo.go",
			want:    true,
		},
		{
			name:    "directory prefix — exact dir path itself matches",
			pattern: "plans",
			path:    "plans",
			want:    true,
		},
		{
			name:    "directory prefix — deep nested path",
			pattern: "plans",
			path:    "plans/uct-123/spring-boot/plan.md",
			want:    true,
		},

		// Glob — * within a single segment
		{
			name:    "glob * matches any filename",
			pattern: "*.lock",
			path:    "package-lock.json",
			want:    false, // .lock suffix not present
		},
		{
			name:    "glob *.lock matches file with .lock suffix",
			pattern: "*.lock",
			path:    "yarn.lock",
			want:    true,
		},
		{
			name:    "glob *.go matches go file",
			pattern: "*.go",
			path:    "main.go",
			want:    true,
		},

		// Glob — * crossing directory separator (Python fnmatch behaviour)
		{
			name:    "glob * crosses directory separator",
			pattern: "src/*.go",
			path:    "src/sub/main.go",
			want:    true,
		},
		{
			name:    "glob *.lock matches file in subdirectory",
			pattern: "*.lock",
			path:    "subdir/yarn.lock",
			want:    true,
		},

		// Glob — ? matches single character
		{
			name:    "glob ? matches single char",
			pattern: "src/fo?.go",
			path:    "src/foo.go",
			want:    true,
		},
		{
			name:    "glob ? does not match empty",
			pattern: "src/fo?.go",
			path:    "src/fo.go",
			want:    false,
		},
		{
			name:    "glob ? does not match two chars",
			pattern: "src/fo?.go",
			path:    "src/fooo.go",
			want:    false,
		},

		// Leading slash normalisation
		{
			name:    "leading slash on path is stripped",
			pattern: "src/config.json",
			path:    "/src/config.json",
			want:    true,
		},
		{
			name:    "leading slash on pattern is stripped",
			pattern: "/src/config.json",
			path:    "src/config.json",
			want:    true,
		},
		{
			name:    "leading slashes on both are stripped",
			pattern: "/plans",
			path:    "/plans/foo.md",
			want:    true,
		},

		// Empty path
		{
			name:    "empty path returns false",
			pattern: "src/foo.go",
			path:    "",
			want:    false,
		},

		// No match — completely different path
		{
			name:    "completely different path",
			pattern: "src/foo.go",
			path:    "test/bar.go",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesPath(tc.pattern, tc.path)
			if got != tc.want {
				t.Errorf("matchesPath(%q, %q) = %v, want %v",
					tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}
