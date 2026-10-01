package install

import (
	"strings"
	"testing"
)

func TestScriptsUseBaseURL(t *testing.T) {
	scripts, err := Scripts("https://versio.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.sh", "install.ps1"} {
		s := string(scripts[name])
		if !strings.Contains(s, "https://versio.example.com") || strings.Contains(s, DefaultBaseURL) {
			t.Errorf("%s: default base URL not replaced", name)
		}
	}
}
