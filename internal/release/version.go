package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version such as v1.4.0 or v2.0.0-rc.1.
type Version struct {
	Major, Minor, Patch uint64
	// Pre holds the dot-separated pre-release identifiers; it is empty for
	// a normal release. Build metadata is ignored, as semver orders it equal.
	Pre []string
}

var versionPattern = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// ParseVersion parses a release tag. The leading "v" is required, as in
// every BurzBlog tag.
func ParseVersion(s string) (Version, error) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a version like v1.4.0", s)
	}
	var v Version
	for i, dst := range []*uint64{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("%q is not a version like v1.4.0: %w", s, err)
		}
		*dst = n
	}
	if m[4] != "" {
		v.Pre = strings.Split(m[4], ".")
	}
	return v, nil
}

// Compare returns -1, 0 or 1 as a is older than, equal to or newer than b,
// following semver precedence.
func Compare(a, b Version) int {
	for _, p := range [][2]uint64{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if c := cmpUint(p[0], p[1]); c != 0 {
			return c
		}
	}
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

// comparePre orders pre-release identifiers: numbers numerically and before
// text, text by bytes.
func comparePre(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		return cmpUint(an, bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
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
