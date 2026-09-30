package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

const validManifest = `{
  "schema": 1,
  "version": "0.2.0",
  "artifacts": {
    "linux/amd64": {
      "url": "../releases/0.2.0/versio_0.2.0_linux_amd64.zip",
      "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
      "size": 10
    }
  }
}`

func TestParseManifestValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	a, ok := m.Artifacts[PlatformKey("linux", "amd64")]
	if m.Version != "0.2.0" || !ok || a.Size != 10 {
		t.Fatalf("unexpected manifest %+v", m)
	}
}

func TestParseManifestInvalid(t *testing.T) {
	cases := map[string]string{
		"malformed":    `{`,
		"schema":       strings.Replace(validManifest, `"schema": 1`, `"schema": 2`, 1),
		"version":      strings.Replace(validManifest, `"version": "0.2.0"`, `"version": "dev"`, 1),
		"no artifacts": `{"schema":1,"version":"0.2.0","artifacts":{}}`,
		"empty url":    strings.Replace(validManifest, `"url": "../releases/0.2.0/versio_0.2.0_linux_amd64.zip"`, `"url": ""`, 1),
		"zero size":    strings.Replace(validManifest, `"size": 10`, `"size": 0`, 1),
		"short sha":    strings.Replace(validManifest, `e3b0c442`, `e3b0`, 1),
		"upper sha":    strings.Replace(validManifest, `e3b0c442`, `E3B0C442`, 1),
		"non-hex sha":  strings.Replace(validManifest, `e3b0c442`, `zzzzzzzz`, 1),
	}
	for name, data := range cases {
		if _, err := ParseManifest([]byte(data)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// sigFile builds a signature file by hand, pinning the wire format rather
// than trusting a signing helper.
func sigFile(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

func TestVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(validManifest)
	sig := sigFile(priv, data)
	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := Verify(pub, append([]byte(" "), data...), sig); err == nil {
		t.Error("signature over modified data accepted")
	}
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := Verify(otherPub, data, sig); err == nil {
		t.Error("signature accepted under the wrong key")
	}
	if err := Verify(pub, data, []byte("not base64!")); err == nil {
		t.Error("malformed signature accepted")
	}
}

func TestParsePublicKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	got, err := ParsePublicKey(base64.StdEncoding.EncodeToString(pub) + "\n")
	if err != nil || !got.Equal(pub) {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, err := ParsePublicKey(base64.StdEncoding.EncodeToString(pub[:16])); err == nil {
		t.Error("short key accepted")
	}
	if _, err := ParsePublicKey("%%%"); err == nil {
		t.Error("malformed key accepted")
	}
}

func TestCompareVersions(t *testing.T) {
	if c, err := CompareVersions("0.10.0", "0.9.0"); err != nil || c != 1 {
		t.Errorf("CompareVersions(0.10.0, 0.9.0) = %d, %v", c, err)
	}
	if _, err := CompareVersions("dev", "0.1.0"); err == nil {
		t.Error("non-semver accepted")
	}
}

func TestValidateVersion(t *testing.T) {
	if err := ValidateVersion("1.2.3-rc.1+build.5"); err != nil {
		t.Error(err)
	}
	for _, v := range []string{"dev", "../1.0.0", "1.0.0/x", "1.0.0-" + strings.Repeat("a", MaxVersionLen)} {
		if err := ValidateVersion(v); err == nil {
			t.Errorf("%q: expected error", v)
		}
	}
}

func TestNaming(t *testing.T) {
	if got := ArchiveName("0.2.0", "windows", "arm64"); got != "versio_0.2.0_windows_arm64.zip" {
		t.Errorf("ArchiveName = %q", got)
	}
	if BinaryName("windows") != "versio.exe" || BinaryName("darwin") != "versio" {
		t.Error("BinaryName mismatch")
	}
}
