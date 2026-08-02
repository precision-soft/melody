package security

import (
    "testing"
)

/* @info this is the record the access-control decision hands to whoever asks why a request was refused, and nothing had ever built one: the whole of it — five accessors and the two copies that keep it from aliasing the compiled rule it describes — was never executed. The attribute slice is copied at BOTH ends on purpose: the rule it is built from is shared by every request of the process, so a caller that sorted or appended to what it was handed would rewrite the authorisation of every request that followed. */
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

/* @info a rule that grants without naming an attribute — a public one — carries an empty list rather than a nil the caller has to guard, so the report reads the same shape whatever matched. */
func TestNewMatchedAccessControlRule_ARuleWithoutAttributesCarriesAnEmptyList(t *testing.T) {
    matched := NewMatchedAccessControlRule("/", nil, SourceGlobal, 0, "")

    if nil == matched.Attributes() {
        t.Fatalf("expected an empty attribute list rather than a nil one")
    }

    if 0 != len(matched.Attributes()) {
        t.Fatalf("expected no attributes, got %v", matched.Attributes())
    }
}
