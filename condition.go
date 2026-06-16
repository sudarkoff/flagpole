package flagpole

import "fmt"

// matchCondition evaluates a GrowthBook-style condition object against attrs.
// Supported: top-level field equality, and the $eq, $ne, $in operators.
// All fields are ANDed. Unsupported operators return an error.
func matchCondition(cond map[string]any, attrs Attributes) (bool, error) {
	for field, expected := range cond {
		actual := attrs[field]
		switch exp := expected.(type) {
		case map[string]any:
			ok, err := matchOperators(exp, actual)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		default:
			if !equalValues(actual, expected) {
				return false, nil
			}
		}
	}
	return true, nil
}

func matchOperators(ops map[string]any, actual any) (bool, error) {
	for op, want := range ops {
		switch op {
		case "$eq":
			if !equalValues(actual, want) {
				return false, nil
			}
		case "$ne":
			if equalValues(actual, want) {
				return false, nil
			}
		case "$in":
			list, ok := want.([]any)
			if !ok {
				return false, fmt.Errorf("flagpole: $in expects an array, got %T", want)
			}
			found := false
			for _, v := range list {
				if equalValues(actual, v) {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		default:
			return false, fmt.Errorf("flagpole: unsupported condition operator %q", op)
		}
	}
	return true, nil
}

// equalValues compares two values with JSON-ish semantics (numbers compared as float64).
func equalValues(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return a == b
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
