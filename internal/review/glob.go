package review

import "strings"

// MatchGlob reports whether a slash-separated path matches a pattern.
//
// The vocabulary is the one people already write in .gitignore and in CI
// configuration:
//
//	**  matches zero or more whole path segments
//	*   matches any run of characters within one segment
//	?   matches one character within one segment
//
// `**` is implemented rather than approximated. filepath.Glob expands it as a
// single directory level, and a `touches` rule written as `**/auth/**` that
// only ever matched one level down would be a protection a repo believes it
// has and does not — the same failure the memory store refuses `**` outright
// to avoid. Here the patterns are matched against a list of changed paths
// rather than a filesystem, so the whole of the semantics is string work.
func MatchGlob(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchSegments walks pattern and path segments together, branching only at
// `**`, where every possible split has to be tried.
func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			// A trailing `**` matches whatever is left, including nothing.
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(pattern[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if !matchSegment(pattern[0], name[0]) {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// matchSegment matches one path segment against one pattern segment, where
// `*` and `?` never cross a separator because there is none left to cross.
func matchSegment(pattern, name string) bool {
	// Fast path: most segments in a real pattern are literals.
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == name
	}
	return matchChars([]rune(pattern), []rune(name))
}

func matchChars(pattern, name []rune) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchChars(pattern[1:], name[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(name) == 0 {
				return false
			}
		default:
			if len(name) == 0 || pattern[0] != name[0] {
				return false
			}
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// MatchAnyGlob reports whether name matches any of the patterns.
//
// A list means "any of", which is what the `matches` operator's own name
// forces. Conjunction over globs is two members of a rule's `all:`.
func MatchAnyGlob(patterns []string, name string) bool {
	for _, p := range patterns {
		if MatchGlob(p, name) {
			return true
		}
	}
	return false
}

// hasEmptySegment reports whether a pattern contains a segment no path segment
// can equal. A leading, trailing or doubled separator produces one, and a
// pattern holding one matches nothing while reading like a rule that does.
func hasEmptySegment(pattern string) bool {
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" {
			return true
		}
	}
	return false
}

// matchesEveryPath reports whether a pattern selects by shape alone, matching
// every path rather than naming any.
//
// Every segment being `*` or `**` is the test, rather than a list of the
// literal patterns that do it: `**`, `**/*`, `*/**` and `**/**` all match every
// path, and a check written against the spellings would keep admitting the next
// one somebody writes.
func matchesEveryPath(pattern string) bool {
	for _, seg := range strings.Split(pattern, "/") {
		if seg != "*" && seg != "**" {
			return false
		}
	}
	return true
}
