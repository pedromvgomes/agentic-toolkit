package githubapp

import "strconv"

// parseInt reads a header GitHub writes as a decimal integer.
func parseInt(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
