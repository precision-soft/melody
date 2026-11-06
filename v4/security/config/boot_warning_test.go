package config

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func bootWarningsNamed(warnings []BootWarning, name string) []BootWarning {
    named := make([]BootWarning, 0)
    for _, warning := range warnings {
        if name == warning.Name {
            named = append(named, warning)
        }
    }

    return named
}

const undeclaredAccessControlRefusal = "security firewall declares no access control and inherits none, so it would authorize every path it claims for every token; declare FirewallConfig.Override.AccessControl, security.NewAccessControl(nil) to keep it open on purpose"

func TestBuilder_RefusesAFirewallNoDeclaredAccessControlReaches(t *testing.T) {
    globalAccessControl := security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{"ROLE_GLOBAL"}})})

    for name, declaration := range map[string]struct {
        global   *security.AccessControl
        override FirewallOverride
    }{
        "overrideOnly":    {global: globalAccessControl, override: FirewallOverride{MergeStrategy: AccessControlMergeOverrideOnly}},
        "notInherited":    {global: globalAccessControl, override: FirewallOverride{GlobalAccessControlDisabled: true}},
        "nothingDeclared": {override: FirewallOverride{}},
        "globalFirst":     {override: FirewallOverride{MergeStrategy: AccessControlMergeGlobalFirst}},
    } {
        builder := NewBuilder()
        builder.SetGlobal(GlobalConfig{AccessControl: declaration.global})
        builder.AddFirewall(name, security.NewPathPrefixMatcher("/one"), &anonymousTokenSource{}, FirewallConfig{Override: declaration.override})

        expectRefusalNamingField(t, undeclaredAccessControlRefusal, "FirewallConfig.Override.AccessControl", func() { builder.BuildAndCompile() })

        if _, compileErr := Compile(Configuration{global: GlobalConfig{AccessControl: declaration.global}, firewalls: builder.firewalls}); nil == compileErr {
            t.Fatalf("%s: expected Compile to refuse", name)
        } else if refusal, isRefusal := compileErr.(*exception.Error); false == isRefusal || name != refusal.Context()["firewallName"] {
            t.Fatalf("%s: expected the refusal to name the firewall, got %v", name, compileErr)
        }
    }
}

func TestBuilder_AdmitsAFirewallADeclaredAccessControlReaches(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{"ROLE_GLOBAL"}})})})

    builder.AddFirewall("openOnPurpose", security.NewPathPrefixMatcher("/three"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{GlobalAccessControlDisabled: true, AccessControl: security.NewAccessControl(nil)}})
    builder.AddFirewall("overrideOnlyOpen", security.NewPathPrefixMatcher("/six"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{MergeStrategy: AccessControlMergeOverrideOnly, AccessControl: security.NewAccessControl(nil)}})
    builder.AddFirewall("inherits", security.NewPathPrefixMatcher("/four"), &anonymousTokenSource{}, FirewallConfig{})
    builder.AddFirewall("declaresItsOwn", security.NewPathPrefixMatcher("/five"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{MergeStrategy: AccessControlMergeOverrideOnly, AccessControl: security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/five", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}})})}})

    if compiled := builder.BuildAndCompile(); 4 != len(compiled.Firewalls()) {
        t.Fatalf("expected the four firewalls compiled, got %d", len(compiled.Firewalls()))
    }

    openGlobalBuilder := NewBuilder()
    openGlobalBuilder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    openGlobalBuilder.AddFirewall("inheritsTheOpenGlobal", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{})
    openGlobalBuilder.AddFirewall("declaresOnlyItsOwn", security.NewPathPrefixMatcher("/x"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{AccessControl: security.NewAccessControl(nil)}})

    if compiled := openGlobalBuilder.BuildAndCompile(); 2 != len(compiled.Firewalls()) {
        t.Fatalf("expected the two open firewalls compiled, got %d", len(compiled.Firewalls()))
    }
}

/* the first matching firewall wins and a path prefix is a string prefix, so a later firewall whose prefix starts with an earlier one's is never selected */
func TestBuilder_BootWarningsNameAFirewallAnEarlierOneShadows(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})

    builder.AddFirewall("api", security.NewPathPrefixMatcher("/api"), &anonymousTokenSource{}, FirewallConfig{})
    builder.AddFirewall("apiAdmin", security.NewPathPrefixMatcher("/api-admin"), &anonymousTokenSource{}, FirewallConfig{})
    builder.AddFirewall("reportsSegment", security.NewPathPrefixMatcher("/reports/"), &anonymousTokenSource{}, FirewallConfig{})
    builder.AddFirewall("reportsArchive", security.NewPathPrefixMatcher("/reportsarchive"), &anonymousTokenSource{}, FirewallConfig{})

    _ = builder.BuildAndCompile()

    warnings := bootWarningsNamed(builder.BootWarnings(), bootWarningShadowedFirewall)
    if 1 != len(warnings) {
        t.Fatalf("expected exactly the firewall /api shadows to be named, got %v", warnings)
    }

    if "apiAdmin" != warnings[0].Context["firewall"] || "api" != warnings[0].Context["shadowedBy"] || "/api-admin" != warnings[0].Context["entry"] {
        t.Fatalf("expected apiAdmin shadowed by api, got %v", warnings[0].Context)
    }

    rootBuilder := NewBuilder()
    rootBuilder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    rootBuilder.AddFirewall("everything", security.NewPathPrefixMatcher(""), &anonymousTokenSource{}, FirewallConfig{})
    rootBuilder.AddFirewall("admin", security.NewPathPrefixMatcher("/admin"), &anonymousTokenSource{}, FirewallConfig{})

    _ = rootBuilder.BuildAndCompile()

    if rootWarnings := bootWarningsNamed(rootBuilder.BootWarnings(), bootWarningShadowedFirewall); 1 != len(rootWarnings) || "admin" != rootWarnings[0].Context["firewall"] {
        t.Fatalf("expected the empty prefix to shadow every later firewall, got %v", rootWarnings)
    }

    orderedBuilder := NewBuilder()
    orderedBuilder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    orderedBuilder.AddFirewall("apiAdmin", security.NewPathPrefixMatcher("/api-admin"), &anonymousTokenSource{}, FirewallConfig{})
    orderedBuilder.AddFirewall("api", security.NewPathPrefixMatcher("/api"), &anonymousTokenSource{}, FirewallConfig{})

    _ = orderedBuilder.BuildAndCompile()

    if orderedWarnings := bootWarningsNamed(orderedBuilder.BootWarnings(), bootWarningShadowedFirewall); 0 != len(orderedWarnings) {
        t.Fatalf("expected the narrower firewall declared first to be silent, got %v", orderedWarnings)
    }
}

/* a request path is matched decoded, so a declared path holding a valid escape other than %2F claims only a resource whose name carries the percent sign */
func TestBuilder_BootWarningsNameADeclaredPathThatCarriesAPercentEscape(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewExactRule("/caf%C3%A9", accesscontrol.RuleConfig{Attributes: []string{"ROLE_GLOBAL"}})})})

    builder.AddFirewall(
        "files",
        security.NewPathPrefixMatcher("/files%41"),
        &anonymousTokenSource{},
        FirewallConfig{
            Override: FirewallOverride{
                GlobalAccessControlDisabled: true,
                AccessControl: security.NewAccessControl([]security.AccessControlRule{
                    accesscontrol.NewExactRule("/files%41/a%20b", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
                    accesscontrol.NewExactRule("/files%41/a%2Fb", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
                    accesscontrol.NewExactRule("/files%41/100%", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
                    accesscontrol.NewRegexRule("^/files%41/x$", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}}),
                }),
            },
        },
    )

    _ = builder.BuildAndCompile()

    warnings := bootWarningsNamed(builder.BootWarnings(), bootWarningPercentEncodedPath)

    entries := make([]string, 0, len(warnings))
    for _, warning := range warnings {
        entries = append(entries, warning.Context["firewall"].(string)+" "+warning.Context["entry"].(string))
    }

    expected := []string{" /caf%C3%A9", "files /files%41", "files /files%41/a%20b", "files /files%41/a%2Fb", "files /files%41/100%"}
    if len(expected) != len(entries) {
        t.Fatalf("expected %v, got %v", expected, entries)
    }

    for index := range expected {
        if expected[index] != entries[index] {
            t.Fatalf("expected %v, got %v", expected, entries)
        }
    }
}

func TestBuilder_BootWarningsNameAFirewallAnEarlierPrefixShadowsAcrossBothMatchers(t *testing.T) {
    cases := []struct {
        name     string
        earlier  securitycontract.Matcher
        later    securitycontract.Matcher
        shadowed bool
    }{
        {"segment over its subtree segment", security.NewSegmentPrefixMatcher("/api"), security.NewSegmentPrefixMatcher("/api/admin"), true},
        {"segment over the same segment", security.NewSegmentPrefixMatcher("/api/"), security.NewSegmentPrefixMatcher("/api"), true},
        {"segment beside a sibling segment", security.NewSegmentPrefixMatcher("/api"), security.NewSegmentPrefixMatcher("/api-admin"), false},
        {"segment over a raw prefix inside the tree", security.NewSegmentPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api/admin"), true},
        {"segment beside a raw prefix of the same spelling", security.NewSegmentPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api"), false},
        {"catch-all segment over a raw prefix", security.NewSegmentPrefixMatcher("/"), security.NewPathPrefixMatcher("/api"), true},
        {"raw prefix over a segment its string begins", security.NewPathPrefixMatcher("/ap"), security.NewSegmentPrefixMatcher("/api"), true},
        {"raw prefix with its slash over the bare segment", security.NewPathPrefixMatcher("/api/"), security.NewSegmentPrefixMatcher("/api"), true},
        {"raw prefix inside the later segment", security.NewPathPrefixMatcher("/api/admin"), security.NewSegmentPrefixMatcher("/api"), false},
        {"empty raw prefix over the catch-all segment", security.NewPathPrefixMatcher(""), security.NewSegmentPrefixMatcher("/"), true},
        {"root raw prefix over the empty raw prefix", security.NewPathPrefixMatcher("/"), security.NewPathPrefixMatcher(""), true},
        {"root raw prefix over the catch-all segment", security.NewPathPrefixMatcher("/"), security.NewSegmentPrefixMatcher("/"), true},
        {"segment over a raw prefix with its slash", security.NewSegmentPrefixMatcher("/api"), security.NewPathPrefixMatcher("/api/"), true},
        {"raw prefix with its slash beside the bare raw prefix", security.NewPathPrefixMatcher("/api/"), security.NewPathPrefixMatcher("/api"), false},
    }

    for _, testCase := range cases {
        builder := NewBuilder()
        builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
        builder.AddFirewall("earlier", testCase.earlier, &anonymousTokenSource{}, FirewallConfig{})
        builder.AddFirewall("later", testCase.later, &anonymousTokenSource{}, FirewallConfig{})

        _ = builder.BuildAndCompile()

        warnings := bootWarningsNamed(builder.BootWarnings(), bootWarningShadowedFirewall)
        if testCase.shadowed != (1 == len(warnings)) {
            t.Fatalf("%s: expected shadowed %v, got %v", testCase.name, testCase.shadowed, warnings)
        }
    }
}
