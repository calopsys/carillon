// Package version implements Carillon's single comparison mechanism: an RE2
// pattern selects candidate tags (a tag must FULLY match to qualify), and an
// ordered list of named capture groups forms an integer comparison key.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Spec is a compiled pattern + comparison key definition.
type Spec struct {
	re      *regexp.Regexp
	compare []string // capture-group names, most significant first
}

// Candidate is a tag that matched a Spec, with its computed integer key.
type Candidate struct {
	Tag string
	Key []int64
}

// Compile builds a Spec. If compare is empty it defaults to every named capture
// group in left-to-right order. Every compare group must exist in the regex.
// The regex is anchored to a full match if it is not already.
func Compile(pattern string, compare []string) (*Spec, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile regex %q: %w", pattern, err)
	}
	names := namedGroups(re)
	if len(names) == 0 {
		return nil, fmt.Errorf("regex %q has no named capture groups", pattern)
	}
	if len(compare) == 0 {
		compare = names
	} else {
		set := make(map[string]bool, len(names))
		for _, n := range names {
			set[n] = true
		}
		for _, c := range compare {
			if !set[c] {
				return nil, fmt.Errorf("compare group %q is not a named group in regex %q", c, pattern)
			}
		}
	}
	return &Spec{re: re, compare: compare}, nil
}

// CompareArity is the number of integer fields in this Spec's comparison key.
func (s *Spec) CompareArity() int { return len(s.compare) }

// Fingerprint identifies the comparison basis: the effective regex plus the
// ordered compare groups — exactly the two things that define what a key's
// integers mean. Two specs with the same fingerprint produce directly
// comparable keys; a change is a signal to re-baseline the stored mark rather
// than compare keys built on different bases. It is a short SHA-256 prefix so it
// stays compact in the stored value and doesn't echo the raw pattern.
func (s *Spec) Fingerprint() string {
	h := sha256.Sum256([]byte(s.re.String() + "\n" + strings.Join(s.compare, ",")))
	return hex.EncodeToString(h[:8])
}

func namedGroups(re *regexp.Regexp) []string {
	var out []string
	for _, n := range re.SubexpNames() {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// Match returns the Candidate for tag if it fully matches the pattern and every
// compare group captured an integer. ok is false for non-candidates (which are
// simply ignored, e.g. git hashes, "latest", or out-of-scope versions).
func (s *Spec) Match(tag string) (c Candidate, ok bool) {
	m := s.re.FindStringSubmatch(tag)
	if m == nil || m[0] != tag { // require a full-string match
		return Candidate{}, false
	}
	idx := s.re.SubexpIndex
	key := make([]int64, len(s.compare))
	for i, name := range s.compare {
		gi := idx(name)
		if gi < 0 || gi >= len(m) {
			return Candidate{}, false
		}
		v, err := strconv.ParseInt(m[gi], 10, 64)
		if err != nil {
			// A non-numeric group means the tag isn't a candidate at all. But an
			// integer that merely overflows int64 (an absurdly long but in-scope
			// version component) must not silently drop the tag and let a smaller
			// one win — clamp it to the max so it still sorts highest.
			if errors.Is(err, strconv.ErrRange) {
				v = math.MaxInt64
			} else {
				return Candidate{}, false // compare group must be integer
			}
		}
		key[i] = v
	}
	return Candidate{Tag: tag, Key: key}, true
}

// Latest returns the highest-keyed candidate among tags. ok is false when no
// tag matches the pattern.
func (s *Spec) Latest(tags []string) (best Candidate, ok bool) {
	for _, t := range tags {
		c, isCand := s.Match(t)
		if !isCand {
			continue
		}
		if !ok || CompareKeys(c.Key, best.Key) > 0 {
			best, ok = c, true
		}
	}
	return best, ok
}

// CompareKeys orders two integer key tuples lexicographically. A shorter key is
// compared on its common prefix; if equal there, the shorter sorts lower. In
// practice the orchestrator only compares keys built from the same Spec (see
// Spec.Fingerprint), so the operands are always equal length; the length
// tie-break is a defensive fallback, not a basis for comparing mismatched
// patterns.
func CompareKeys(a, b []int64) int {
	n := min(len(a), len(b))
	for i := range n {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
