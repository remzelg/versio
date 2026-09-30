// Package signing creates Ed25519 keys and release signatures. It is the
// counterpart of release.Verify in the client, and the only code that ever
// handles the private key.
package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Sign returns the detached signature file contents for data, in the format
// release.Verify expects: the base64 Ed25519 signature and a newline.
func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

// EncodeKey returns the base64 text form used for key files and for the
// public key embedded in the launcher (see release.ParsePublicKey).
func EncodeKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

// ParsePrivateKey decodes a base64 Ed25519 private key as written by EncodeKey.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, errors.New("malformed private key") // don't echo key material
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key is %d bytes, want %d", len(b), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(b), nil
}
