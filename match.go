package main

import (
	"regexp"
	"strings"
)

// matchesPath reports whether filePath should be stripped based on
// filterPattern.  The logic mirrors the regex produced by pathToRegex so
// that tests can verify matching behaviour without invoking git-filter-repo.
//
// Rules:
//
//   - Exact match:        "src/foo.go"   matches filterPattern "src/foo.go"
//   - Directory prefix:   "src/gen/a.go" matches filterPattern "src/gen"
//   - Glob (fnmatch):     "src/foo.lock" matches filterPattern "*.lock"
//     Python fnmatch allows * to cross directory separators, so a single *
//     can match a path containing /.
//
// Both arguments are normalised by stripping any leading slash before
// comparison.
func matchesPath(filterPattern, filePath string) bool {
	if filePath == "" {
		return false
	}
	filePath = strings.TrimPrefix(filePath, "/")
	re := pathToRegexCompiled(filterPattern)
	return re.MatchString(filePath)
}

// pathToRegex converts a filter pattern into a Python regex string suitable
// for passing to git-filter-repo's --path-regex flag.
//
// The regex matches:
//   - The exact path (e.g. "src/config.json")
//   - Any path that is a child of the pattern treated as a directory prefix
//     (e.g. "src/generated/foo.go" for pattern "src/generated")
//   - Glob patterns using fnmatch semantics where * and ? may cross
//     directory separators (e.g. "*.lock" matches "subdir/yarn.lock")
//
// The regex is anchored (^…) so it matches from the start of the path.
func pathToRegex(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "/")
	base := strings.TrimSuffix(pattern, "/")

	// Determine whether the pattern looks like a glob (contains * or ?).
	// If not, we produce a simple literal+prefix regex.
	if !strings.ContainsAny(pattern, "*?") {
		// Match the exact path or any path beneath it as a directory.
		// re.escape equivalent: escape regex metacharacters in the literal part.
		escaped := regexEscape(base)
		return "^" + escaped + "($|/)"
	}

	// Glob pattern: translate fnmatch wildcards to regex.
	// * → .* (crosses directory separators, matching Python fnmatch behaviour)
	// ? → .  (matches any single character)
	// All other regex metacharacters are escaped.
	return "^" + fnmatchToRegex(base) + "$"
}

// regexEscape escapes all Python regex metacharacters in s.
func regexEscape(s string) string {
	const metachars = `\.+*?^${}()|[]`
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(metachars, c) {
			b.WriteRune('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// fnmatchToRegex converts a glob pattern (with * and ?) to a regex fragment.
func fnmatchToRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			if strings.ContainsRune(`\.+^${}()|[]`, rune(pattern[i])) {
				b.WriteByte('\\')
			}
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}

// pathToRegexCompiled compiles the pattern for use by matchesPath.
func pathToRegexCompiled(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pathToRegex(pattern))
}
