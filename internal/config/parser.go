package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// ParseFile reads and decodes a consumer config file. It validates the
// minimum needed to make the decoded value safe for the resolver to act
// on: source URL is non-empty, every external has a non-empty URL, every
// listed platform is known, and no unknown fields appear.
func ParseFile(path string) (*ConsumerConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	return parseBytes(path, raw)
}

func parseBytes(path string, raw []byte) (*ConsumerConfig, error) {
	var cfg ConsumerConfig
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		line, col := extractYAMLPos(err)
		kind := classifyYAMLError(err)
		return nil, &ParseError{
			Path:    path,
			Line:    line,
			Column:  col,
			Kind:    kind,
			Message: cleanYAMLMessage(err),
			Wrapped: err,
		}
	}
	if cfg.Source.URL == "" {
		return nil, newErr(path, ErrMissingRequired, "source.url is required")
	}
	for i, ext := range cfg.Externals {
		if ext.URL == "" {
			return nil, newErr(path, ErrInvalidSource,
				"externals[%d]: url is required", i)
		}
	}
	for _, p := range cfg.Platforms {
		if !definitions.IsKnownPlatform(p) {
			return nil, newErr(path, ErrUnknownPlatform,
				"unknown platform %q (known: %v)", p, definitions.AllPlatforms)
		}
	}
	return &cfg, nil
}

// classifyYAMLError mirrors internal/definitions: strict mode emits
// unknown-field errors with a recognisable message; everything else is
// generic syntax.
func classifyYAMLError(err error) ErrorKind {
	msg := err.Error()
	if strings.Contains(msg, "unknown field") {
		return ErrUnknownField
	}
	return ErrYAMLSyntax
}

var yamlPosRE = regexp.MustCompile(`\[(\d+):(\d+)\]`)

func extractYAMLPos(err error) (int, int) {
	if se, ok := err.(*yaml.SyntaxError); ok {
		if t := se.Token; t != nil && t.Position != nil {
			return t.Position.Line, t.Position.Column
		}
	}
	m := yamlPosRE.FindStringSubmatch(err.Error())
	if len(m) == 3 {
		var l, c int
		fmt.Sscanf(m[1], "%d", &l)
		fmt.Sscanf(m[2], "%d", &c)
		return l, c
	}
	return 0, 0
}

func cleanYAMLMessage(err error) string {
	s := err.Error()
	if i := strings.Index(s, "\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
