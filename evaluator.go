package flagpole

import "fmt"

// Result is the outcome of evaluating a feature.
type Result struct {
	Value any  // resolved value (force value, variation value, or defaultValue)
	On    bool // truthiness of Value

	// Experiment metadata (zero-valued for non-experiment results).
	VariationID   int    // assigned variation index; meaningful only when InExperiment
	InExperiment  bool   // true only on a genuine hash-based assignment
	HashAttribute string // attribute used for bucketing
	HashValue     string // the actual unit value bucketed (the join key)
	ExperimentKey string // matched experiment rule's Key; set only when InExperiment
}

// Evaluate resolves a feature for the given attributes. Rules are tried in
// order; the first one that fully matches wins. featureKey is used as the
// default rollout seed (matching GrowthBook).
func Evaluate(f Feature, featureKey string, attrs Attributes) Result {
	for _, rule := range f.Rules {
		if rule.Condition != nil {
			ok, err := matchCondition(rule.Condition, attrs)
			if err != nil {
				continue // a rule we cannot understand must not silently apply
			}
			if !ok {
				continue
			}
		}

		// Experiment rule: variations present => assign a variation.
		if len(rule.Variations) >= 2 {
			if r, matched := assignExperiment(rule, attrs); matched {
				return r
			}
			continue // not bucketed in => fall through to next rule / default
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

// assignExperiment buckets a unit into an experiment rule. matched is false when
// the unit is not in the experiment (no identifier, unsupported hash version, or
// outside coverage), in which case the caller falls through.
func assignExperiment(rule Rule, attrs Attributes) (Result, bool) {
	// Note: rule.Ranges / rule.Namespace are not evaluated here. The compat
	// suite skips such rules via usesUnsupported; direct-API callers are not.
	// hashVersion 2 only. A nil hashVersion is treated as v2 (flagpole's
	// default), which diverges from GrowthBook's nil-default of v1: callers
	// porting GrowthBook experiment JSON should set hashVersion:2 explicitly.
	// An explicit other version is outside our subset.
	if rule.HashVersion != nil && *rule.HashVersion != 2 {
		return Result{}, false
	}
	hashAttr := rule.HashAttribute
	if hashAttr == "" {
		hashAttr = "id"
	}
	hashValue := stringAttr(attrs[hashAttr])
	if hashValue == "" {
		return Result{}, false // no identifier => never in an experiment
	}
	seed := rule.Seed
	if seed == "" {
		seed = rule.Key // experiment key is the default seed
	}
	coverage := 1.0
	if rule.Coverage != nil {
		coverage = *rule.Coverage
	}
	ranges := getBucketRanges(len(rule.Variations), coverage, rule.Weights)
	i := chooseVariation(hashV2(seed, hashValue), ranges)
	if i < 0 {
		return Result{}, false // outside coverage
	}
	val := rule.Variations[i]
	return Result{
		Value:         val,
		On:            truthy(val),
		VariationID:   i,
		InExperiment:  true,
		HashAttribute: hashAttr,
		HashValue:     hashValue,
		ExperimentKey: rule.Key,
	}, true
}

func inCoverage(rule Rule, featureKey string, attrs Attributes) bool {
	// We implement hashVersion 2 only. A rule that explicitly requests any other
	// version (including unknown versions, which GrowthBook skips) is outside our
	// supported subset, so the rollout rule does not apply.
	if rule.HashVersion != nil && *rule.HashVersion != 2 {
		return false
	}
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
	case int:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	default:
		return true
	}
}
