package config

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

    /** AllowAnonymous must carry the public-access attribute, otherwise the access-control listener treats an empty attribute set as "authentication required" and denies anonymous users. */
    attributes, matched = accessControl.Match("/public")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if 1 != len(attributes) || securitycontract.AttributePublicAccess != attributes[0] {
        t.Fatalf("expected anonymous rule to carry the public-access attribute, got %v", attributes)
    }
}
