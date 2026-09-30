package signing

import (
	"crypto/ed25519"
	"testing"

	"github.com/remycarr/versio/client/release"
)

// TestClientAcceptsSignatures checks Sign against the client's own verifier.
func TestClientAcceptsSignatures(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"schema":1}`)
	if err := release.Verify(pub, data, Sign(priv, data)); err != nil {
		t.Fatalf("client rejected signature: %v", err)
	}
	// The encoded public key is what gets embedded in the launcher.
	got, err := release.ParsePublicKey(EncodeKey(pub))
	if err != nil || !got.Equal(pub) {
		t.Fatalf("client rejected encoded public key: %v", err)
	}
}

func TestParsePrivateKey(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	got, err := ParsePrivateKey(EncodeKey(priv) + "\n")
	if err != nil || !got.Equal(priv) || !got.Public().(ed25519.PublicKey).Equal(pub) {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, err := ParsePrivateKey(EncodeKey(pub)); err == nil {
		t.Error("public key accepted as private key")
	}
	if _, err := ParsePrivateKey("%%%"); err == nil {
		t.Error("malformed key accepted")
	}
}
