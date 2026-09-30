package semver

import "testing"

func TestParseInvalid(t *testing.T) {
	for _, s := range []string{
		"", "dev", "1", "1.2", "1.2.3.4", "v1.2.3", "01.2.3", "1.02.3",
		"1.2.3-", "1.2.3-01", "1.2.3-a..b", "1.2.3+", "1.2.3-a_b", "-1.2.3", "1.2.x",
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): expected error", s)
		}
	}
}

func TestCompareOrdering(t *testing.T) {
	// Ascending, from the semver.org precedence example plus extras.
	ordered := []string{
		"0.9.9",
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
		"1.0.1",
		"1.1.0",
		"2.0.0",
		"10.0.0",
	}
	for i := range ordered {
		for j := range ordered {
			a, err := Parse(ordered[i])
			if err != nil {
				t.Fatal(err)
			}
			b, err := Parse(ordered[j])
			if err != nil {
				t.Fatal(err)
			}
			want := cmpUint(uint64(i), uint64(j))
			if got := Compare(a, b); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

func TestCompareIgnoresBuildMetadata(t *testing.T) {
	a, _ := Parse("1.0.0+build.1")
	b, _ := Parse("1.0.0+build.2")
	if Compare(a, b) != 0 {
		t.Fatal("build metadata should not affect ordering")
	}
}
