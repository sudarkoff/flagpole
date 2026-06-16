package flagpole

import (
	"fmt"
	"reflect"
	"strings"
)

// matchCondition evaluates a GrowthBook-style condition object against attrs.
// Supported: top-level field equality, and the $eq, $ne, $in operators.
// All fields are ANDed. Unsupported operators return an error.
func matchCondition(cond map[string]any, attrs Attributes) (bool, error) {
	for field, expected := range cond {
		if strings.HasPrefix(field, "$") {
			return false, fmt.Errorf("flagpole: unsupported condition operator %q", field)
		}
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
			if !inList(actual, list) {
				return false, nil
			}
		default:
			return false, fmt.Errorf("flagpole: unsupported condition operator %q", op)
		}
	}
	return true, nil
}

// inList reports whether actual matches the $in list. If actual is itself a
// slice, it matches when any element is in the list (GrowthBook intersection
// semantics); otherwise it matches on scalar membership.
func inList(actual any, list []any) bool {
	if arr, ok := actual.([]any); ok {
		for _, a := range arr {
			for _, v := range list {
				if equalValues(a, v) {
					return true
				}
			}
		}
		return false
	}
	for _, v := range list {
		if equalValues(actual, v) {
			return true
		}
	}
	return false
}

// equalValues compares two values with JSON-ish semantics (numbers compared as
// float64, slices/maps via reflect.DeepEqual to avoid panic on uncomparable types).
func equalValues(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	// Use reflect.DeepEqual for slices and maps to avoid runtime panics when
	// comparing uncomparable types through interface{}.
	switch a.(type) {
	case []any, map[string]any:
		return reflect.DeepEqual(a, b)
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
