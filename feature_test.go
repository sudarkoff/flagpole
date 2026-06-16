package flagpole

import (
	"encoding/json"
	"testing"
)

func TestFeatureJSONRoundTrip(t *testing.T) {
	raw := `{
		"defaultValue": false,
		"rules": [
			{"condition": {"plan": "starter"}, "force": true},
			{"coverage": 0.5, "hashAttribute": "id", "force": true}
		]
	}`
	var f Feature
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.DefaultValue != false {
		t.Errorf("defaultValue = %v", f.DefaultValue)
	}
	if len(f.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(f.Rules))
	}
	if f.Rules[1].Coverage == nil || *f.Rules[1].Coverage != 0.5 {
		t.Errorf("coverage = %v", f.Rules[1].Coverage)
	}
}
