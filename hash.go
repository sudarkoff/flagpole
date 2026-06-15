package flagpole

import (
	"strconv"
	"unicode/utf16"
)

// fnv32a matches JavaScript's FNV-1a over UTF-16 code units (String.charCodeAt),
// which is what GrowthBook's reference SDKs use.
func fnv32a(s string) uint32 {
	const prime = 0x01000193
	var h uint32 = 0x811c9dc5
	for _, u := range utf16.Encode([]rune(s)) {
		h ^= uint32(u)
		h *= prime
	}
	return h
}

// hashV2 is GrowthBook's hashVersion 2: a double FNV-1a producing a value in [0,1).
func hashV2(seed, value string) float64 {
	h := fnv32a(strconv.FormatUint(uint64(fnv32a(seed+value)), 10))
	return float64(h%10000) / 10000.0
}
