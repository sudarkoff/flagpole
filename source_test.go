package flagpole

import (
	"context"
	"testing"
)

func TestStaticSourceFromJSON(t *testing.T) {
	payload := `{"features": {"f1": {"defaultValue": true}}}`
	src, err := StaticSourceFromJSON([]byte(payload))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	feats, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := feats["f1"]; !ok {
		t.Fatalf("missing feature f1")
	}
}
