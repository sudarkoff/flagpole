package flagpole

import (
	"encoding/json"
	"math"
	"testing"
)

func approxRanges(a, b [][2]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i][0]-b[i][0]) > 1e-9 || math.Abs(a[i][1]-b[i][1]) > 1e-9 {
			return false
		}
	}
	return true
}

func TestCompatGetEqualWeights(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["getEqualWeights"], &cases); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range cases {
		var n int
		_ = json.Unmarshal(c[0], &n)
		var want []float64
		_ = json.Unmarshal(c[1], &want)
		got := getEqualWeights(n)
		if len(got) != len(want) {
			t.Errorf("getEqualWeights(%d) len = %d, want %d", n, len(got), len(want))
			continue
		}
		for i := range want {
			if math.Abs(got[i]-want[i]) > 1e-9 {
				t.Errorf("getEqualWeights(%d)[%d] = %v, want %v", n, i, got[i], want[i])
			}
		}
	}
}

func TestCompatGetBucketRange(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["getBucketRange"], &cases); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)
		var args []json.RawMessage
		_ = json.Unmarshal(c[1], &args)
		var n int
		_ = json.Unmarshal(args[0], &n)
		var coverage float64
		_ = json.Unmarshal(args[1], &coverage)
		var weights []float64
		_ = json.Unmarshal(args[2], &weights) // null -> nil
		var want [][2]float64
		_ = json.Unmarshal(c[2], &want)
		got := getBucketRanges(n, coverage, weights)
		if !approxRanges(got, want) {
			t.Errorf("%s: getBucketRanges = %v, want %v", name, got, want)
		}
	}
}

func TestCompatChooseVariation(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["chooseVariation"], &cases); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)
		var bucket float64
		_ = json.Unmarshal(c[1], &bucket)
		var ranges [][2]float64
		_ = json.Unmarshal(c[2], &ranges)
		var want int
		_ = json.Unmarshal(c[3], &want)
		if got := chooseVariation(bucket, ranges); got != want {
			t.Errorf("%s: chooseVariation(%v) = %d, want %d", name, bucket, got, want)
		}
	}
}
