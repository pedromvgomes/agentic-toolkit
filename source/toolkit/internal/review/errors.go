package review

import (
	"errors"
	"fmt"
)

// ParseError is returned for any failure reading or validating a review
// manifest. It carries the source path, the field inside it when one is
// known, and line/column when the YAML layer reported them.
type ParseError struct {
	Path string
	// Field is the dotted path to the offending field, e.g.
	// `panels.deep.reviewers[2]`. Empty when the failure is the document's
	// rather than a field's.
	Field   string
	Line    int
	Column  int
	Kind    ErrorKind
	Message string
	Wrapped error
}

// ErrorKind classifies failures so callers and tests can branch without
// string-matching messages.
type ErrorKind string

const (
	ErrIO               ErrorKind = "io"
	ErrYAMLSyntax       ErrorKind = "yaml_syntax"
	ErrUnknownField     ErrorKind = "unknown_field"
	ErrUnknownVersion   ErrorKind = "unknown_version"
	ErrMissingRequired  ErrorKind = "missing_required"
	ErrUnknownName      ErrorKind = "unknown_name"
	ErrInvalidCondition ErrorKind = "invalid_condition"
	ErrInvalidPrompt    ErrorKind = "invalid_prompt"
	ErrInvalidPanel     ErrorKind = "invalid_panel"
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
	if e.Field != "" {
		return fmt.Sprintf("%s: %s: %s: %s", loc, e.Field, e.Kind, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", loc, e.Kind, e.Message)
}

func (e *ParseError) Unwrap() error { return e.Wrapped }

// IsKind reports whether err, or anything it wraps, is a *ParseError of the
// given kind.
func IsKind(err error, kind ErrorKind) bool {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe.Kind == kind
	}
	return false
}

// fieldErr builds a ParseError against one field.
func fieldErr(path, field string, kind ErrorKind, format string, args ...interface{}) *ParseError {
	return &ParseError{Path: path, Field: field, Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// wrapFieldErr builds a ParseError against one field, keeping the underlying
// error reachable through errors.Is/As.
func wrapFieldErr(path, field string, kind ErrorKind, err error) *ParseError {
	return &ParseError{Path: path, Field: field, Kind: kind, Message: err.Error(), Wrapped: err}
}
