// Package sourceref carries the canonical (URL, Ref) pair that identifies a
// fetchable git source. It is intentionally tiny and dependency-free so the
// stack, resolver, sourcestore, and lockfile packages can share one Source
// type without introducing import cycles.
package sourceref

import (
	"fmt"
	"strings"
)

// Source identifies a fetchable git source at an optional ref.
//
// In YAML it accepts two forms (UnmarshalYAML normalises them):
//
//	github.com/owner/repo@main          # shorthand
//	{ url: github.com/owner/repo, ref: main }
//
// URL may include an in-repo path joined by `.git/` for direct-ref fetches
// (e.g. github.com/owner/repo.git/skills/foo). Empty Ref means the resolver
// should pick the source's default branch.
type Source struct {
	URL string `yaml:"url"`
	Ref string `yaml:"ref,omitempty"`
}

// UnmarshalYAML accepts both the shorthand string form '<url>[@<ref>]' and
// the explicit {url, ref} mapping. Used by the stack parser when decoding
// each entry in `extends:` and the per-category lists.
func (s *Source) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err == nil {
		url, ref, ok := ParseShorthand(str)
		if !ok {
			return fmt.Errorf("source shorthand %q must be '<url>[@<ref>]' with a non-empty url", str)
		}
		s.URL = url
		s.Ref = ref
		return nil
	}
	type alias Source
	var a alias
	if err := unmarshal(&a); err != nil {
		return err
	}
	*s = Source(a)
	return nil
}

// ParseShorthand returns (url, ref, ok). It rejects the empty string and any
// form whose URL portion is empty (e.g. "@main"). Splits on the rightmost
// '@'.
func ParseShorthand(s string) (url, ref string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		url = s[:i]
		ref = s[i+1:]
	} else {
		url = s
	}
	if url == "" {
		return "", "", false
	}
	return url, ref, true
}
