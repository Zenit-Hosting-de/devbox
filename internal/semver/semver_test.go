package semver

import "testing"

func TestCompare(t *testing.T) {
	order := []string{"0.0.1", "0.1.0", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.2.3", "2.0.0"}
	for i := 0; i < len(order)-1; i++ {
		a, b := mustParse(t, order[i]), mustParse(t, order[i+1])
		if Compare(a, b) >= 0 || Compare(b, a) <= 0 {
			t.Errorf("%s should sort before %s", order[i], order[i+1])
		}
	}
	if Compare(mustParse(t, "v1.0.0+build.1"), mustParse(t, "1.0.0")) != 0 {
		t.Error("build metadata must be ignored")
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1.2.x", "1.2.3-"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): want an error", s)
		}
	}
}

func TestRange(t *testing.T) {
	tests := []struct {
		rng     string
		match   []string
		noMatch []string
	}{
		{">=1.0.0 <2.0.0", []string{"1.0.0", "1.9.9"}, []string{"0.9.9", "2.0.0", "1.5.0-rc.1"}},
		{"^1.2.0", []string{"1.2.0", "1.9.0"}, []string{"1.1.9", "2.0.0"}},
		{"^0.2.3", []string{"0.2.3", "0.2.9"}, []string{"0.3.0"}},
		{"^0.0.3", []string{"0.0.3"}, []string{"0.0.4"}},
		{"~1.4.0", []string{"1.4.0", "1.4.7"}, []string{"1.5.0"}},
		{"1.2.3", []string{"1.2.3"}, []string{"1.2.4"}},
		{"<1.0.0 || >=3.0.0", []string{"0.5.0", "3.1.0"}, []string{"1.0.0", "2.9.9"}},
		{">=1.0.0-rc.1", []string{"1.0.0-rc.2", "1.0.0", "2.0.0"}, []string{"1.0.0-beta.1", "1.1.0-rc.1"}},
	}
	for _, tt := range tests {
		r, err := ParseRange(tt.rng)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", tt.rng, err)
		}
		for _, v := range tt.match {
			if !r.Match(mustParse(t, v)) {
				t.Errorf("%q should match %s", tt.rng, v)
			}
		}
		for _, v := range tt.noMatch {
			if r.Match(mustParse(t, v)) {
				t.Errorf("%q should not match %s", tt.rng, v)
			}
		}
	}
	for _, s := range []string{"", ">=", "^x", "1.0 || "} {
		if _, err := ParseRange(s); err == nil {
			t.Errorf("ParseRange(%q): want an error", s)
		}
	}
}

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
