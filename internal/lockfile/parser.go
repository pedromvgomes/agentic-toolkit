package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// ParseFile reads and decodes a lockfile. It validates the version tag,
// rejects unknown fields, and requires url/ref/sha on every source.
func ParseFile(path string) (*Lockfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrIO, Message: err.Error(), Wrapped: err}
	}
	return parseBytes(path, raw)
}

func parseBytes(path string, raw []byte) (*Lockfile, error) {
	var lf Lockfile
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&lf); err != nil {
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
	if lf.Version == 0 {
		return nil, newErr(path, ErrMissingRequired, "version is required")
	}
	if lf.Version != Version {
		hint := ""
		if lf.Version == 1 {
			hint = " (v1 was the consumer-config + preset schema; run `agtk lock` to regenerate as v2)"
		}
		return nil, newErr(path, ErrUnsupportedVersion,
			"lockfile version %d is not supported (this build supports %d)%s", lf.Version, Version, hint)
	}
	for i, s := range lf.Sources {
		if s.URL == "" {
			return nil, newErr(path, ErrMissingRequired, "sources[%d].url is required", i)
		}
		if s.Ref == "" {
			return nil, newErr(path, ErrMissingRequired, "sources[%d].ref is required", i)
		}
		if s.SHA == "" {
			return nil, newErr(path, ErrMissingRequired, "sources[%d].sha is required", i)
		}
	}
	return &lf, nil
}

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
