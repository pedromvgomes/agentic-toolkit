package config

import (
	"errors"
	"fmt"
)

// ParseError carries the source path and (when known) line/column for any
// failure when reading a consumer config file.
type ParseError struct {
	Path    string
	Line    int
	Column  int
	Kind    ErrorKind
	Message string
	Wrapped error
}

// ErrorKind classifies parser failures so callers (and tests) can branch
// without string-matching messages.
type ErrorKind string

const (
	ErrIO              ErrorKind = "io"
	ErrYAMLSyntax      ErrorKind = "yaml_syntax"
	ErrUnknownField    ErrorKind = "unknown_field"
	ErrMissingRequired ErrorKind = "missing_required"
	ErrUnknownPlatform ErrorKind = "unknown_platform"
	ErrInvalidSource   ErrorKind = "invalid_source"
)

func (e *ParseError) Error() string {
	loc := e.Path
	if e.Line > 0 {
		if e.Column > 0 {
			loc = fmt.Sprintf("%s:%d:%d", e.Path, e.Line, e.Column)
		} else {
			loc = fmt.Sprintf("%s:%d", e.Path, e.Line)
		}
	}
	return fmt.Sprintf("%s: %s: %s", loc, e.Kind, e.Message)
}

func (e *ParseError) Unwrap() error { return e.Wrapped }

// IsKind reports whether err (or any wrapped error) is a *ParseError of the
// given kind.
func IsKind(err error, kind ErrorKind) bool {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe.Kind == kind
	}
	return false
}

func newErr(path string, kind ErrorKind, format string, args ...interface{}) *ParseError {
	return &ParseError{
		Path:    path,
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	}
}
