package memory

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/goccy/go-yaml"
)

// frontmatterDelim matches a `---` line, optionally padded and CRLF-ended.
var frontmatterDelim = regexp.MustCompile(`(?m)^---[ \t]*\r?\n`)

// ParseNote decodes one note file. file is recorded on the note and used in
// error messages; it is not read from.
//
// Decoding is strict: an unknown frontmatter key is an error rather than a
// silently ignored field, because a typo'd `anchor:` would otherwise
// produce a note that looks anchored and is never checked.
func ParseNote(file string, raw []byte) (*Note, error) {
	fm, body, err := splitFrontmatter(file, raw)
	if err != nil {
		return nil, err
	}

	var n Note
	dec := yaml.NewDecoder(bytes.NewReader(fm), yaml.Strict())
	if err := dec.Decode(&n); err != nil {
		return nil, fmt.Errorf("%s: frontmatter: %w", file, err)
	}
	n.Body = string(body)
	n.File = file
	return &n, nil
}

// splitFrontmatter returns the YAML block and the body that follows it.
func splitFrontmatter(file string, raw []byte) (fm, body []byte, err error) {
	open := frontmatterDelim.FindIndex(raw)
	if open == nil || open[0] != 0 {
		return nil, nil, fmt.Errorf("%s: must begin with a YAML frontmatter block delimited by ---", file)
	}
	rest := raw[open[1]:]
	closing := frontmatterDelim.FindIndex(rest)
	if closing == nil {
		return nil, nil, fmt.Errorf("%s: frontmatter opening --- is not followed by a closing ---", file)
	}
	return rest[:closing[0]], rest[closing[1]:], nil
}

// Render serialises the note back to its on-disk form: regenerated
// frontmatter followed by the untouched body. Only `agtk memory anchor`
// writes notes, and it goes through here.
//
// The body survives byte-for-byte; the frontmatter does not. It is
// re-marshalled from the struct, so YAML comments and field order inside it
// are replaced by the canonical form on the first stamp. Anything worth
// saying about a note belongs in its body.
func (n *Note) Render() ([]byte, error) {
	// IndentSequence keeps `anchors:` list items indented under their key,
	// which is how notes are written by hand; without it every stamp would
	// reflow the frontmatter and show up as noise in review.
	fm, err := yaml.MarshalWithOptions(n, yaml.IndentSequence(true))
	if err != nil {
		return nil, fmt.Errorf("marshal frontmatter for %q: %w", n.Name, err)
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	b.WriteString(n.Body)
	return b.Bytes(), nil
}
