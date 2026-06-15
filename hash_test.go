package flagpole

import "testing"

func TestFNV32a(t *testing.T) {
	// Reference values from GrowthBook's hashing spec.
	cases := map[string]uint32{
		"":  0x811c9dc5,
		"a": 0xe40c292c,
	}
	for in, want := range cases {
		if got := fnv32a(in); got != want {
			t.Errorf("fnv32a(%q) = %#x, want %#x", in, got, want)
		}
	}
}

func TestHashV2Structural(t *testing.T) {
	// Structural guarantees. Exact GrowthBook-vector equality is asserted in
	// Task 6 against the vendored cases.json (the authoritative oracle).
	for _, c := range []struct{ seed, value string }{
		{"", "a"}, {"", "b"}, {"a", "a"}, {"seed", "value"},
	} {
		got := hashV2(c.seed, c.value)
		if got < 0 || got >= 1 {
			t.Errorf("hashV2(%q,%q) = %v, want in [0,1)", c.seed, c.value, got)
		}
		if got != hashV2(c.seed, c.value) {
			t.Errorf("hashV2(%q,%q) is not deterministic", c.seed, c.value)
		}
	}
	// Different inputs should generally produce different buckets.
	if hashV2("", "a") == hashV2("", "b") {
		t.Error("expected distinct buckets for distinct values")
	}
}
