package flagpole

import (
	"context"
	"testing"
)

func TestClientEvaluate(t *testing.T) {
	src := StaticSource{Features: map[string]Feature{
		"f1": {DefaultValue: false, Rules: []Rule{
			{Condition: map[string]any{"plan": "starter"}, Force: true},
		}},
		"greeting": {DefaultValue: "hello"},
	}}
	c, err := New(context.Background(), src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if !c.For(Attributes{"id": "u1", "plan": "starter"}).IsOn("f1") {
		t.Error("f1 should be on for starter")
	}
	if c.For(Attributes{"id": "u1", "plan": "free"}).IsOn("f1") {
		t.Error("f1 should be off for free")
	}
	if got := c.For(Attributes{"id": "u1"}).Value("greeting", "x"); got != "hello" {
		t.Errorf("greeting = %v, want hello", got)
	}
	if got := c.For(Attributes{"id": "u1"}).Value("missing", "fallback"); got != "fallback" {
		t.Errorf("missing = %v, want fallback", got)
	}
}

func TestClientUnknownFlagIsOff(t *testing.T) {
	c, err := New(context.Background(), StaticSource{Features: map[string]Feature{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.For(Attributes{"id": "u1"}).IsOn("nope") {
		t.Error("unknown flag must be off")
	}
}
