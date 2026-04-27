package definitions

import (
	"errors"
	"fmt"
)

// ParseError is returned for any parser failure. It carries the source path
// and, when known, the line/column inside the file.
type ParseError struct {
	Path    string
	Line    int // 1-based; 0 if unknown
	Column  int // 1-based; 0 if unknown
	Kind    ErrorKind
	Message string
	// Wrapped is the underlying YAML error or io error, if any.
	Wrapped error
}

// ErrorKind classifies parser failures so callers (and tests) can branch
// without string-matching messages.
type ErrorKind string

const (
	ErrIO                ErrorKind = "io"
	ErrFrontmatterMissing ErrorKind = "frontmatter_missing"
	ErrFrontmatterUnclosed ErrorKind = "frontmatter_unclosed"
	ErrYAMLSyntax        ErrorKind = "yaml_syntax"
	ErrUnknownField      ErrorKind = "unknown_field"
	ErrMissingRequired   ErrorKind = "missing_required"
	ErrUnknownPlatform   ErrorKind = "unknown_platform"
	ErrUnknownColor      ErrorKind = "unknown_color"
	ErrUnknownTransport  ErrorKind = "unknown_transport"
	ErrUnknownHandler    ErrorKind = "unknown_handler"
	ErrInvalidName       ErrorKind = "invalid_name"
	ErrTransportConflict ErrorKind = "transport_conflict"
	ErrPlatformExtension ErrorKind = "platform_extension"
	ErrHandlerShape      ErrorKind = "handler_shape"
	ErrUnknownCategory   ErrorKind = "unknown_category"
	ErrPresetMalformedRef ErrorKind = "preset_malformed_ref"
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
// given kind. Useful in tests.
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

func newErrAt(path string, line, col int, kind ErrorKind, format string, args ...interface{}) *ParseError {
	return &ParseError{
		Path:    path,
		Line:    line,
		Column:  col,
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	}
}
