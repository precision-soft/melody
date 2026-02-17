package config

import "testing"

func TestAccessControlBuilder_BuildMatchesRules(t *testing.T) {
	builder := NewAccessControlBuilder()

	builder.Require("/admin", "ROLE_ADMIN").AllowAnonymous("/")

	accessControl := builder.Build()

	attributes, matched := accessControl.Match("/admin/panel")
	if false == matched {
		t.Fatalf("expected matched")
	}
	if 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
		t.Fatalf("unexpected attributes")
	}

	attributes, matched = accessControl.Match("/public")
	if false == matched {
		t.Fatalf("expected matched")
	}
	if 0 != len(attributes) {
		t.Fatalf("expected anonymous (no attributes)")
	}
}
