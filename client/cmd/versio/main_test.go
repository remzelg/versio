package main

import (
	"bytes"
	"testing"
)

func runWith(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	old := version
	version = "0.1.0"
	t.Cleanup(func() { version = old })

	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestNoArgs(t *testing.T) {
	code, out, errs := runWith(t)
	if code != 0 || out != "Your version is 0.1.0\n" || errs != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestVersionFlag(t *testing.T) {
	code, out, errs := runWith(t, "--version")
	if code != 0 || out != "0.1.0\n" || errs != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestUnsupportedArgs(t *testing.T) {
	for _, args := range [][]string{{"unexpected"}, {"-v"}, {"version"}, {"--help"}, {"--version", "extra"}} {
		code, out, errs := runWith(t, args...)
		if code == 0 || out != "" || errs != "usage: versio [--version]\n" {
			t.Errorf("args %q: got code=%d stdout=%q stderr=%q", args, code, out, errs)
		}
	}
}
