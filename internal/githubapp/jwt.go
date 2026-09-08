package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// jwtLifetime is how long an App JWT is offered for.
//
// GitHub refuses one whose expiry is more than ten minutes out, and refusing
// is what it does with the clock on its own side. Nine minutes leaves the
// margin that difference needs.
const jwtLifetime = 9 * time.Minute

// jwtBackdate is how far the issued-at is set behind the caller's clock, for
// the same reason: a machine running a few seconds fast otherwise issues a
// token GitHub reads as being from the future and rejects.
const jwtBackdate = 60 * time.Second

// appJWT mints the assertion that proves this process holds the App's key.
//
// Written here rather than taken from a library: it is one header, three
// claims and an RS256 signature, and a dependency that parses and verifies
// arbitrary tokens is a great deal of surface for a program that only ever
// signs its own.
func (c *Credential) appJWT(now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("build the App token header: %w", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-jwtBackdate).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		// GitHub accepts the App id as a string as readily as a number, and
		// a string cannot lose precision on the way through a JSON reader.
		"iss": strconv.FormatInt(c.AppID, 10),
	})
	if err != nil {
		return "", fmt.Errorf("build the App token claims: %w", err)
	}

	signing := encodeSegment(header) + "." + encodeSegment(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign the App token: %w", err)
	}
	return signing + "." + encodeSegment(sig), nil
}

// encodeSegment is JWT's base64: URL alphabet, no padding.
func encodeSegment(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
