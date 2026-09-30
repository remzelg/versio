// Package semver parses and orders Semantic Versioning 2.0.0 strings
// (https://semver.org) such as "1.2.3" or "1.2.3-rc.1+build.5".
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semantic version. Build metadata is discarded because
// it does not participate in ordering.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string // pre-release identifiers, e.g. ["rc", "1"]
}

// Parse parses s as a semantic version without a leading "v".
func Parse(s string) (Version, error) {
	core, build, hasBuild := strings.Cut(s, "+")
	if hasBuild {
		if err := checkIdentifiers(build, false); err != nil {
			return Version{}, fmt.Errorf("semver %q: build metadata: %w", s, err)
		}
	}
	core, pre, hasPre := strings.Cut(core, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("semver %q: want MAJOR.MINOR.PATCH", s)
	}
	var nums [3]uint64
	for i, p := range parts {
		n, err := parseNumeric(p)
		if err != nil {
			return Version{}, fmt.Errorf("semver %q: %w", s, err)
		}
		nums[i] = n
	}

	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if hasPre {
		if err := checkIdentifiers(pre, true); err != nil {
			return Version{}, fmt.Errorf("semver %q: pre-release: %w", s, err)
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, nil
}

// Compare returns -1, 0, or +1 as a is lower than, equal to, or higher than b.
func Compare(a, b Version) int {
	for _, c := range [...][2]uint64{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if c[0] != c[1] {
			return cmpUint(c[0], c[1])
		}
	}
	// A version without pre-release identifiers ranks above one with them.
	switch {
	case len(a.Pre) == 0 && len(b.Pre) == 0:
		return 0
	case len(a.Pre) == 0:
		return 1
	case len(b.Pre) == 0:
		return -1
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		if c := comparePre(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(a.Pre)), uint64(len(b.Pre)))
}

// comparePre orders two pre-release identifiers: numeric identifiers compare
// numerically and rank below alphanumeric ones, which compare lexically.
func comparePre(a, b string) int {
	aNum, bNum := isDigits(a), isDigits(b)
	switch {
	case aNum && bNum:
		// No leading zeros, so a longer number is a larger one.
		if len(a) != len(b) {
			return cmpUint(uint64(len(a)), uint64(len(b)))
		}
		return strings.Compare(a, b)
	case aNum:
		return -1
	case bNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func parseNumeric(s string) (uint64, error) {
	if s == "" || !isDigits(s) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%q has a leading zero", s)
	}
	return strconv.ParseUint(s, 10, 64)
}

// checkIdentifiers validates dot-separated identifiers of [0-9A-Za-z-].
// Pre-release numeric identifiers additionally may not have leading zeros.
func checkIdentifiers(s string, pre bool) error {
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return fmt.Errorf("empty identifier in %q", s)
		}
		for _, r := range id {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
				return fmt.Errorf("invalid character %q in %q", r, id)
			}
		}
		if pre && isDigits(id) && len(id) > 1 && id[0] == '0' {
			return fmt.Errorf("%q has a leading zero", id)
		}
	}
	return nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
