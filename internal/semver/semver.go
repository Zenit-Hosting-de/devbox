// Package semver parses Semantic Versioning 2.0 versions and the version ranges
// used in manifests and profiles: comparisons (>, >=, <, <=, =), caret (^1.2.0) and
// tilde (~1.2.0), space-separated for "and", "||" for "or".
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed version. Build metadata is ignored.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string
}

// Parse parses "1.2.3" or "1.2.3-rc.1+build"; a leading "v" is accepted.
func Parse(s string) (Version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q: want MAJOR.MINOR.PATCH", s)
	}
	var nums [3]uint64
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("version %q: invalid number %q", s, p)
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("version %q: invalid number %q", s, p)
		}
		nums[i] = n
	}
	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if hasPre {
		if pre == "" {
			return Version{}, fmt.Errorf("version %q: empty pre-release", s)
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Compare returns -1, 0 or 1.
func Compare(a, b Version) int {
	for _, d := range [][2]uint64{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
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
	switch {
	case len(a.Pre) < len(b.Pre):
		return -1
	case len(a.Pre) > len(b.Pre):
		return 1
	}
	return 0
}

func comparePre(a, b string) int {
	an, aErr := strconv.ParseUint(a, 10, 64)
	bn, bErr := strconv.ParseUint(b, 10, 64)
	switch {
	case aErr == nil && bErr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aErr == nil:
		return -1 // numeric identifiers sort before alphanumeric ones
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

type comparator struct {
	op string
	v  Version
}

func (c comparator) match(v Version) bool {
	d := Compare(v, c.v)
	switch c.op {
	case ">":
		return d > 0
	case ">=":
		return d >= 0
	case "<":
		return d < 0
	case "<=":
		return d <= 0
	default:
		return d == 0
	}
}

// Range is a parsed version range.
type Range struct {
	text string
	any  [][]comparator // or of and
}

func (r Range) String() string { return r.text }

// ParseRange parses a range such as ">=1.0.0 <2.0.0", "^1.2.0" or "~1.4.0 || >=2.1.0".
func ParseRange(s string) (Range, error) {
	r := Range{text: s}
	for _, alt := range strings.Split(s, "||") {
		fields := strings.Fields(alt)
		if len(fields) == 0 {
			return Range{}, fmt.Errorf("range %q: empty alternative", s)
		}
		var all []comparator
		for _, f := range fields {
			cs, err := parseComparator(f)
			if err != nil {
				return Range{}, fmt.Errorf("range %q: %w", s, err)
			}
			all = append(all, cs...)
		}
		r.any = append(r.any, all)
	}
	return r, nil
}

func parseComparator(f string) ([]comparator, error) {
	for _, op := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		rest, ok := strings.CutPrefix(f, op)
		if !ok {
			continue
		}
		v, err := Parse(rest)
		if err != nil {
			return nil, err
		}
		switch op {
		case "^":
			upper := Version{Major: v.Major + 1}
			if v.Major == 0 {
				upper = Version{Minor: v.Minor + 1}
				if v.Minor == 0 {
					upper = Version{Patch: v.Patch + 1}
				}
			}
			return []comparator{{">=", v}, {"<", upper}}, nil
		case "~":
			return []comparator{{">=", v}, {"<", Version{Major: v.Major, Minor: v.Minor + 1}}}, nil
		default:
			return []comparator{{op, v}}, nil
		}
	}
	v, err := Parse(f)
	if err != nil {
		return nil, err
	}
	return []comparator{{"=", v}}, nil
}

// Match reports whether v is in the range. Pre-releases match only comparators
// that name a pre-release of the same MAJOR.MINOR.PATCH, as in npm.
func (r Range) Match(v Version) bool {
	for _, all := range r.any {
		ok := true
		for _, c := range all {
			if !c.match(v) {
				ok = false
				break
			}
		}
		if ok && (len(v.Pre) == 0 || allowsPre(all, v)) {
			return true
		}
	}
	return false
}

func allowsPre(all []comparator, v Version) bool {
	for _, c := range all {
		if len(c.v.Pre) > 0 && c.v.Major == v.Major && c.v.Minor == v.Minor && c.v.Patch == v.Patch {
			return true
		}
	}
	return false
}
