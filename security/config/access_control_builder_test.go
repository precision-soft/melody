package config

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/security/contract"
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

    attributes, matched = accessControl.Match("/public")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if 1 != len(attributes) || securitycontract.AttributePublicAccess != attributes[0] {
        t.Fatalf("expected anonymous rule to carry the public-access attribute, got %v", attributes)
    }
}

func TestAccessControlBuilder_AllowAnonymousMatchesOnSegmentBoundary(t *testing.T) {
    accessControl := NewAccessControlBuilder().AllowAnonymous("/api/public").Build()

    if _, matched := accessControl.Match("/api/public"); false == matched {
        t.Fatalf("expected the declared anonymous prefix itself to match")
    }
    if _, matched := accessControl.Match("/api/public/health"); false == matched {
        t.Fatalf("expected a child path of the anonymous prefix to match")
    }

    if _, matched := accessControl.Match("/api/public-data"); true == matched {
        t.Fatalf("a sibling path sharing a string prefix must NOT be opened anonymously")
    }
    if _, matched := accessControl.Match("/api/publicXYZ/secret"); true == matched {
        t.Fatalf("a sibling path sharing a string prefix must NOT be opened anonymously")
    }
}
