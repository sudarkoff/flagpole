package flagpole

import "math"

// getEqualWeights returns n equal weights summing to 1. n<=0 yields an empty
// slice (matches GrowthBook).
func getEqualWeights(n int) []float64 {
	if n <= 0 {
		return []float64{}
	}
	w := make([]float64, n)
	for i := range w {
		w[i] = 1.0 / float64(n)
	}
	return w
}

// round6 rounds to 6 decimal places, matching GrowthBook's bucket-range
// rounding so boundaries compare cleanly against the fixtures.
func round6(f float64) float64 { return math.Round(f*1e6) / 1e6 }

// getBucketRanges returns cumulative [start,end) ranges, one per variation,
// each scaled by coverage. Weights are equalized when absent, the wrong length,
// or not summing to ~1 (GrowthBook semantics).
func getBucketRanges(numVariations int, coverage float64, weights []float64) [][2]float64 {
	if coverage < 0 {
		coverage = 0
	}
	if coverage > 1 {
		coverage = 1
	}
	if len(weights) != numVariations {
		weights = getEqualWeights(numVariations)
	} else {
		sum := 0.0
		for _, w := range weights {
			sum += w
		}
		if sum < 0.99 || sum > 1.01 {
			weights = getEqualWeights(numVariations)
		}
	}
	ranges := make([][2]float64, len(weights))
	cumulative := 0.0
	for i, w := range weights {
		start := cumulative
		cumulative += w
		ranges[i] = [2]float64{round6(start), round6(start + coverage*w)}
	}
	return ranges
}

// chooseVariation returns the index of the range containing bucket, or -1 when
// the bucket falls in no range (not in the experiment).
func chooseVariation(bucket float64, ranges [][2]float64) int {
	for i, r := range ranges {
		if bucket >= r[0] && bucket < r[1] {
			return i
		}
	}
	return -1
}
