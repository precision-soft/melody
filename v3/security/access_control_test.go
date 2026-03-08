package security

import (
    "reflect"
    "strings"
    "testing"
)

func TestAccessControl_Match_ReturnsFalseWhenNoRules(t *testing.T) {
    control := NewAccessControl()

    attributes, matched := control.Match("/admin")
    if true == matched {
        t.Fatalf("expected not matched")
    }
    if 0 != len(attributes) {
        t.Fatalf("expected empty attributes")
    }
}

func TestAccessControl_Match_LongestPrefixWins(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
        NewAccessControlRule("/admin/settings", "ROLE_SETTINGS"),
    )

    attributes, matched := control.Match("/admin/settings/users")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_SETTINGS" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControl_Match_FallbackRuleWhenPrefixEmpty(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("", "ROLE_ANY"),
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )

    attributes, matched := control.Match("/public")
    if false == matched {
        t.Fatalf("expected matched by fallback")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ANY" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControlRule_NormalizesPrefixAndFiltersEmptyAttributes(t *testing.T) {
    rule := NewAccessControlRule("/admin/", "ROLE_ADMIN", "", "   ", "ROLE_USER")

    control := NewAccessControl(rule)

    attributes, matched := control.Match("/admin/dashboard")
    if false == matched {
        t.Fatalf("expected matched")
    }

    if 2 != len(attributes) {
        t.Fatalf("expected two attributes")
    }

    if "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
    if "ROLE_USER" != attributes[1] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControl_Match_EmptyPathNormalizedToRoot(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/", "ROLE_ROOT"),
    )

    attributes, matched := control.Match("")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ROOT" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestMatchAccessControlRule_SetsMetadataCorrectly(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
        NewAccessControlRule("/admin/settings", "ROLE_SETTINGS"),
    )

    matchedRule, attributes, matched := matchAccessControlRule(control, "/admin/settings/users", SourceFirewall, "main")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedRule {
        t.Fatalf("expected matched rule")
    }

    if "/admin/settings" != matchedRule.PathPrefix() {
        t.Fatalf("unexpected path prefix")
    }
    if SourceFirewall != matchedRule.Source() {
        t.Fatalf("unexpected source")
    }
    if "main" != matchedRule.Firewall() {
        t.Fatalf("unexpected firewall")
    }
    if 1 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected rule index")
    }

    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_SETTINGS" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestMatchAccessControlRule_NormalizesEmptyPathToRoot(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/", "ROLE_ROOT"),
    )

    matchedRule, attributes, matched := matchAccessControlRule(control, "", SourceGlobal, "")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedRule {
        t.Fatalf("expected matched rule")
    }
    if "/" != matchedRule.PathPrefix() {
        t.Fatalf("unexpected path prefix")
    }
    if 0 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected rule index")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ROOT" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestMatchAccessControlRule_FallbackRuleSelectedOnce(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("", "ROLE_ANY"),
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )

    matchedRule, attributes, matched := matchAccessControlRule(control, "/public", SourceGlobal, "")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedRule {
        t.Fatalf("expected matched rule")
    }
    if "" != matchedRule.PathPrefix() {
        t.Fatalf("unexpected path prefix")
    }
    if 0 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected rule index")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ANY" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControlMatch_ExactWinsBeforePrefixAndRegex(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/", "ROLE_USER"),
        NewAccessControlRule("/products", "ROLE_EDITOR"),
        NewAccessControlExactRule("/", "PUBLIC_ACCESS"),
    )

    attributes, ok := accessControl.Match("/")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"PUBLIC_ACCESS"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }

    attributes, ok = accessControl.Match("/products")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes = []string{"ROLE_EDITOR"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_ExactRuleMatchesTrailingSlashPath(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlExactRule("/admin", "ROLE_ADMIN"),
    )

    attributes, ok := accessControl.Match("/admin/")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"ROLE_ADMIN"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_PrefixLongestWinsBeforeRegex(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/", "ROLE_USER"),
        NewAccessControlRule("/products", "ROLE_EDITOR"),
        NewAccessControlRule("/products/api", "ROLE_ADMIN"),
    )

    attributes, ok := accessControl.Match("/products/api/read")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"ROLE_ADMIN"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_RegexFirstMatchWinsInDeclarationOrder(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/products", "A"),
        NewAccessControlRegexRule("^/products/api", "B"),
    )

    attributes, ok := accessControl.Match("/products/api/read")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"A"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_FallbackEmptyPrefixIsUsedLast(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRule("/login", "PUBLIC_ACCESS"),
        NewAccessControlRule("", "ROLE_USER"),
    )

    attributes, ok := accessControl.Match("/login")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"PUBLIC_ACCESS"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }

    attributes, ok = accessControl.Match("/anything")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes = []string{"ROLE_USER"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_EmptyPathIsNormalizedToSlash(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlExactRule("/", "PUBLIC_ACCESS"),
        NewAccessControlRegexRule("^/", "ROLE_USER"),
    )

    attributes, ok := accessControl.Match("")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"PUBLIC_ACCESS"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestRules_ReturnsCopy(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRule("/login", "PUBLIC_ACCESS"),
        NewAccessControlRule("/products", "ROLE_EDITOR"),
    )

    rules := accessControl.Rules()
    if 2 != len(rules) {
        t.Fatalf("expected 2 rules, got %d", len(rules))
    }

    rules[0] = NewAccessControlRule("/changed", "X")

    rulesAfter := accessControl.Rules()
    if true == strings.HasPrefix(rulesAfter[0].pathPrefix, "/changed") {
        t.Fatalf("expected returned rules to be a copy")
    }
}

func TestNewAccessControlRegexRule_EmptyPatternPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewAccessControlRegexRule("", "PUBLIC_ACCESS")
}

func TestNewAccessControlRegexRule_InvalidPatternPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewAccessControlRegexRule("(", "PUBLIC_ACCESS")
}

func TestNewAccessControlExactRule_EmptyPathPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewAccessControlExactRule("", "PUBLIC_ACCESS")
}
