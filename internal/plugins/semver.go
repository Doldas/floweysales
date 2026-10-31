package plugins

import (
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?$`)

// ValidVersion reports whether v is MAJOR.MINOR.PATCH[-PRERELEASE].
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }

// IsPrerelease reports whether v carries a pre-release suffix.
func IsPrerelease(v string) bool { return strings.Contains(v, "-") }

// CompareVersions orders semantic versions: -1, 0 or 1. Build metadata is not
// supported; a pre-release sorts before its release. Invalid versions sort
// before every valid one.
func CompareVersions(a, b string) int {
	ma, mb := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	switch {
	case ma == nil && mb == nil:
		return strings.Compare(a, b)
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		if c := compareNumeric(ma[i], mb[i]); c != 0 {
			return c
		}
	}
	switch {
	case ma[4] == mb[4]:
		return 0
	case ma[4] == "":
		return 1
	case mb[4] == "":
		return -1
	}
	pa, pb := strings.Split(ma[4], "."), strings.Split(mb[4], ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		switch {
		case errA == nil && errB == nil:
			if na != nb {
				return sign(na - nb)
			}
		case errA == nil: // numeric identifiers sort before alphanumeric ones
			return -1
		case errB == nil:
			return 1
		default:
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(pa) - len(pb))
}

func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		return sign(len(a) - len(b))
	}
	return strings.Compare(a, b)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
