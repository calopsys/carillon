package version

import "testing"

func TestSemverFilterAndLatest(t *testing.T) {
	s, err := Compile(`^v?(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$`, []string{"major", "minor", "patch"})
	if err != nil {
		t.Fatal(err)
	}
	// v2.0.0-rc1 is not a full match (trailing -rc1); noise tags are dropped.
	tags := []string{"v1.2.0", "v1.10.0", "v1.9.4", "latest", "sha-abcdef0", "v2.0.0-rc1", "1.2.3"}
	best, ok := s.Latest(tags)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if best.Tag != "v1.10.0" {
		t.Fatalf("expected v1.10.0 (10 > 9 > 2 numerically), got %q", best.Tag)
	}
}

func TestNumericNotLexicographic(t *testing.T) {
	s, _ := Compile(`^v?(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$`, nil)
	a, _ := s.Match("v1.9.0")
	b, _ := s.Match("v1.10.0")
	if CompareKeys(b.Key, a.Key) <= 0 {
		t.Fatalf("1.10.0 must sort above 1.9.0")
	}
}

func TestScopeNarrowingRegex(t *testing.T) {
	// pin to the v1 line only
	s, err := Compile(`^v1\.(?P<minor>\d+)\.(?P<patch>\d+)$`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Match("v2.0.0"); ok {
		t.Fatal("v2.0.0 must not be a candidate under a v1-pinned pattern")
	}
	best, ok := s.Latest([]string{"v1.4.0", "v2.0.0", "v1.5.1", "v3.0.0"})
	if !ok || best.Tag != "v1.5.1" {
		t.Fatalf("expected v1.5.1, got %q ok=%v", best.Tag, ok)
	}
	if s.CompareArity() != 2 {
		t.Fatalf("expected arity 2 (minor,patch), got %d", s.CompareArity())
	}
}

func TestComparePriorityReordersGroups(t *testing.T) {
	// DD-MM-YYYY captured, but compared year-first
	s, err := Compile(`^(?P<day>\d+)-(?P<month>\d+)-(?P<year>\d+)$`, []string{"year", "month", "day"})
	if err != nil {
		t.Fatal(err)
	}
	best, ok := s.Latest([]string{"31-12-2024", "01-01-2025", "15-06-2025"})
	if !ok || best.Tag != "15-06-2025" {
		t.Fatalf("expected 15-06-2025, got %q", best.Tag)
	}
}

func TestMinioReleasePattern(t *testing.T) {
	s, err := Compile(`^RELEASE\.(?P<year>\d+)-(?P<month>\d+)-(?P<day>\d+)T.*Z$`, []string{"year", "month", "day"})
	if err != nil {
		t.Fatal(err)
	}
	best, ok := s.Latest([]string{
		"RELEASE.2025-09-30T01-02-03Z",
		"RELEASE.2025-10-15T00-00-00Z",
		"latest",
	})
	if !ok || best.Tag != "RELEASE.2025-10-15T00-00-00Z" {
		t.Fatalf("expected the Oct release, got %q", best.Tag)
	}
}

func TestOptionalComponentsDefaultZero(t *testing.T) {
	// One tracker, mixed shapes: X.Y, X.Y.Z, X.Y.Z-rev. Absent optional numeric
	// components count as 0, giving a constant-arity key that orders correctly.
	s, err := Compile(
		`^v?(?P<major>\d+)\.(?P<minor>\d+)(?:\.(?P<patch>\d+))?(?:-(?P<rev>\d+))?$`,
		[]string{"major", "minor", "patch", "rev"},
	)
	if err != nil {
		t.Fatal(err)
	}

	keys := map[string][]int64{
		"1.28":     {1, 28, 0, 0},
		"1.27.1":   {1, 27, 1, 0},
		"1.27.1-2": {1, 27, 1, 2},
	}
	for tag, want := range keys {
		c, ok := s.Match(tag)
		if !ok {
			t.Fatalf("%s should be a candidate", tag)
		}
		if len(c.Key) != len(want) {
			t.Fatalf("%s key %v, want %v", tag, c.Key, want)
		}
		for i := range want {
			if c.Key[i] != want[i] {
				t.Fatalf("%s key %v, want %v", tag, c.Key, want)
			}
		}
	}

	// The real-world case: a higher minor outranks a revision on a lower patch.
	if best, ok := s.Latest([]string{"1.27.1", "1.27.1-2", "1.28"}); !ok || best.Tag != "1.28" {
		t.Fatalf("expected 1.28 newest, got %q ok=%v", best.Tag, ok)
	}
	// But a revision genuinely ranks above the base patch when minor is equal.
	if best, ok := s.Latest([]string{"1.27.1", "1.27.1-2"}); !ok || best.Tag != "1.27.1-2" {
		t.Fatalf("expected 1.27.1-2 newest, got %q ok=%v", best.Tag, ok)
	}

	// Relaxing absent groups to 0 must not loosen matching: an optional patch is
	// still \d+, so ".x" fails the full-match and the tag is dropped.
	if _, ok := s.Match("1.27.x"); ok {
		t.Fatal("1.27.x must not be a candidate (patch must be numeric)")
	}
}

func TestUnknownCompareGroupErrors(t *testing.T) {
	_, err := Compile(`^v(?P<major>\d+)$`, []string{"minor"})
	if err == nil {
		t.Fatal("expected error for unknown compare group")
	}
}

func TestNoCandidates(t *testing.T) {
	s, _ := Compile(`^v(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)$`, nil)
	if _, ok := s.Latest([]string{"latest", "main", "sha-deadbeef"}); ok {
		t.Fatal("expected no candidates")
	}
}
