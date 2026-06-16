// Package flagpole evaluates feature flags locally using a GrowthBook-compatible
// algorithm: the same FNV-1a v2 hashing and a strict subset of GrowthBook's
// feature schema, so definitions and bucketing port to GrowthBook unchanged.
//
// Typical use:
//
//	c, _ := flagpole.New(ctx, src) // src is a Source (e.g. sourcepg.New(pool))
//	defer c.Close()
//	if c.For(flagpole.Attributes{"id": userID, "plan": plan}).IsOn("my-flag") {
//	    // ...
//	}
package flagpole
