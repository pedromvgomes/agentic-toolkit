// Package config models the consumer-side config file. A consumer repo
// declares which toolkit source(s) to render, which platforms to target,
// and which preset bundles to apply via .agentic-toolkit/config.yaml.
//
// Slice-1 scope: source, platforms, externals, presets. Includes/excludes
// and per-definition overrides are deferred.
package config

import (
	"fmt"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// ConsumerConfig is the deserialised .agentic-toolkit/config.yaml.
type ConsumerConfig struct {
	Source    Source                 `yaml:"source"               agtkdoc:"required;Primary toolkit source. Accepts shorthand '<url>[@<ref>]' or struct {url, ref}."`
	Platforms []definitions.Platform `yaml:"platforms,omitempty"  agtkdoc:"Render only for these platforms. Empty means render every platform supported by each definition."`
	Externals []Source               `yaml:"externals,omitempty"  agtkdoc:"Additional sources to fetch from. Same shorthand or struct form as 'source'."`
	Presets   []string               `yaml:"presets,omitempty"    agtkdoc:"Named preset bundles from the primary source, in stacking order. Last entry wins on conflict (resolver semantic)."`
}

// Source identifies a fetchable toolkit repository at an optional git ref.
//
// In YAML it accepts two forms, normalised at parse time:
//
//	source: github.com/owner/repo@main          # shorthand
//	source: { url: github.com/owner/repo, ref: main }
//
// The parser does not crack the URL, classify the ref, or check
// reachability — those are the resolver's responsibility.
type Source struct {
	URL string `yaml:"url"           agtkdoc:"required;Repository URL (e.g. github.com/owner/repo)."`
	Ref string `yaml:"ref,omitempty" agtkdoc:"Optional git ref (branch, tag, or sha). Empty means the resolver chooses the default branch."`
}

// UnmarshalYAML accepts the shorthand string form '<url>[@<ref>]' as well
// as the explicit {url, ref} mapping. The intentional duplication of the
// '@<ref>' splitter with internal/definitions is documented in the slice
// design — these are independent grammars that happen to overlap today.
func (s *Source) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err == nil {
		url, ref, ok := parseSourceShorthand(str)
		if !ok {
			return fmt.Errorf("source shorthand %q must be '<url>[@<ref>]' with a non-empty url", str)
		}
		s.URL = url
		s.Ref = ref
		return nil
	}
	// Fall back to the struct form. Use an alias to avoid recursion.
	type alias Source
	var a alias
	if err := unmarshal(&a); err != nil {
		return err
	}
	*s = Source(a)
	return nil
}

// parseSourceShorthand returns (url, ref, ok). It rejects the empty string
// and any form whose URL portion is empty (e.g. "@main").
func parseSourceShorthand(s string) (url, ref string, ok bool) {
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
