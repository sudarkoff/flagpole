package flagpole

import "fmt"

// Result is the outcome of evaluating a feature.
type Result struct {
	Value any  // resolved value (force value or defaultValue)
	On    bool // truthiness of Value
}

// Evaluate resolves a feature for the given attributes. Rules are tried in
// order; the first one that fully matches wins. featureKey is used as the
// default rollout seed (matching GrowthBook).
func Evaluate(f Feature, featureKey string, attrs Attributes) Result {
	for _, rule := range f.Rules {
		if rule.Condition != nil {
			ok, err := matchCondition(rule.Condition, attrs)
			if err != nil {
				// A rule we cannot understand must not silently apply; skip it.
				continue
			}
			if !ok {
				continue
			}
		}
		if rule.Coverage != nil {
			if !inCoverage(rule, featureKey, attrs) {
				continue
			}
		}
		// Rule applies. A rule with neither force nor coverage is a plain match.
		val := rule.Force
		if val == nil {
			val = f.DefaultValue
		}
		return Result{Value: val, On: truthy(val)}
	}
	return Result{Value: f.DefaultValue, On: truthy(f.DefaultValue)}
}

func inCoverage(rule Rule, featureKey string, attrs Attributes) bool {
	hashAttr := rule.HashAttribute
	if hashAttr == "" {
		hashAttr = "id"
	}
	value := stringAttr(attrs[hashAttr])
	if value == "" {
		return false // no identifier => never in a partial rollout
	}
	seed := rule.Seed
	if seed == "" {
		seed = featureKey
	}
	return hashV2(seed, value) < *rule.Coverage
}

func stringAttr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != "" && x != "false" && x != "0"
	case float64:
		return x != 0
	default:
		return true
	}
}
