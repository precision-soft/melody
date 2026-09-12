package security

import (
    "testing"
)

func TestNewMatchedAccessControlRule_CarriesTheMatchAndCopiesTheAttributesAtBothEnds(t *testing.T) {
    callerAttributes := []string{"ROLE_EDITOR", "ROLE_ADMIN"}

    matched := NewMatchedAccessControlRule("/admin", callerAttributes, SourceFirewall, 3, "main")

    if "/admin" != matched.PathPrefix() {
        t.Fatalf("unexpected path prefix: %q", matched.PathPrefix())
    }

    if SourceFirewall != matched.Source() {
        t.Fatalf("unexpected source: %v", matched.Source())
    }

    if 3 != matched.RuleIndex() {
        t.Fatalf("unexpected rule index: %d", matched.RuleIndex())
    }

    if "main" != matched.Firewall() {
        t.Fatalf("unexpected firewall: %q", matched.Firewall())
    }

    callerAttributes[0] = "ROLE_ANONYMOUS"

    if "ROLE_EDITOR" != matched.Attributes()[0] {
        t.Fatalf("expected the record to hold its own copy of the attributes, got %v", matched.Attributes())
    }

    handedOut := matched.Attributes()
    handedOut[0] = "ROLE_ANONYMOUS"

    if "ROLE_EDITOR" != matched.Attributes()[0] {
        t.Fatalf("expected a caller's mutation of what it was handed not to reach the record, got %v", matched.Attributes())
    }
}

func TestNewMatchedAccessControlRule_ARuleWithoutAttributesCarriesAnEmptyList(t *testing.T) {
    matched := NewMatchedAccessControlRule("/", nil, SourceGlobal, 0, "")

    if nil == matched.Attributes() {
        t.Fatalf("expected an empty attribute list rather than a nil one")
    }

    if 0 != len(matched.Attributes()) {
        t.Fatalf("expected no attributes, got %v", matched.Attributes())
    }
}
