package config

import (
    "testing"

    "github.com/precision-soft/melody/v3/security"
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

/* a firewall left with the empty access control the compile built authorizes every path it claims, so it is named; one that declares the empty control on purpose, or inherits or declares a rule, is silent */
func TestBuilder_BootWarningsNameTheFirewallWhoseAccessControlTheCompileLeftEmpty(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(security.NewAccessControl(security.NewAccessControlRule("/", "ROLE_GLOBAL")), nil, nil, nil, nil)

    builder.AddStatelessFirewall("overrideOnly", security.NewPathPrefixMatcher("/one"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration().WithMergeStrategy(AccessControlMergeOverrideOnly))
    builder.AddStatelessFirewall("notInherited", security.NewPathPrefixMatcher("/two"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration().WithInheritGlobalAccessControl(false))
    builder.AddStatelessFirewall("openOnPurpose", security.NewPathPrefixMatcher("/three"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration().WithInheritGlobalAccessControl(false).WithAccessControl(security.NewAccessControl()))
    builder.AddStatelessFirewall("inherits", security.NewPathPrefixMatcher("/four"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())
    builder.AddStatelessFirewall("declaresItsOwn", security.NewPathPrefixMatcher("/five"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration().WithMergeStrategy(AccessControlMergeOverrideOnly).WithAccessControl(security.NewAccessControl(security.NewAccessControlRule("/five", "ROLE_LOCAL"))))

    _ = builder.BuildAndCompile()

    warnings := bootWarningsNamed(builder.BootWarnings(), bootWarningEmptyFirewallAccessControl)
    if 2 != len(warnings) || "overrideOnly" != warnings[0].Context["firewall"] || "notInherited" != warnings[1].Context["firewall"] {
        t.Fatalf("expected the two firewalls the compile left empty to be named, got %v", warnings)
    }
}

/* the first matching firewall wins and a path prefix is a string prefix, so a later firewall whose prefix starts with an earlier one's is never selected */
func TestBuilder_BootWarningsNameAFirewallAnEarlierOneShadows(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall("api", security.NewPathPrefixMatcher("/api"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())
    builder.AddStatelessFirewall("apiAdmin", security.NewPathPrefixMatcher("/api-admin"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())
    builder.AddStatelessFirewall("reportsSegment", security.NewPathPrefixMatcher("/reports/"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())
    builder.AddStatelessFirewall("reportsArchive", security.NewPathPrefixMatcher("/reportsarchive"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())

    _ = builder.BuildAndCompile()

    warnings := bootWarningsNamed(builder.BootWarnings(), bootWarningShadowedFirewall)
    if 1 != len(warnings) {
        t.Fatalf("expected exactly the firewall /api shadows to be named, got %v", warnings)
    }

    if "apiAdmin" != warnings[0].Context["firewall"] || "api" != warnings[0].Context["shadowedBy"] || "/api-admin" != warnings[0].Context["entry"] {
        t.Fatalf("expected apiAdmin shadowed by api, got %v", warnings[0].Context)
    }

    rootBuilder := NewBuilder()
    rootBuilder.AddStatelessFirewall("everything", security.NewPathPrefixMatcher(""), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())
    rootBuilder.AddStatelessFirewall("admin", security.NewPathPrefixMatcher("/admin"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())

    _ = rootBuilder.BuildAndCompile()

    if rootWarnings := bootWarningsNamed(rootBuilder.BootWarnings(), bootWarningShadowedFirewall); 1 != len(rootWarnings) || "admin" != rootWarnings[0].Context["firewall"] {
        t.Fatalf("expected the empty prefix to shadow every later firewall, got %v", rootWarnings)
    }

    orderedBuilder := NewBuilder()
    orderedBuilder.AddStatelessFirewall("apiAdmin", security.NewPathPrefixMatcher("/api-admin"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())
    orderedBuilder.AddStatelessFirewall("api", security.NewPathPrefixMatcher("/api"), nil, &anonymousTokenSource{}, NewFirewallOverrideConfiguration())

    _ = orderedBuilder.BuildAndCompile()

    if orderedWarnings := bootWarningsNamed(orderedBuilder.BootWarnings(), bootWarningShadowedFirewall); 0 != len(orderedWarnings) {
        t.Fatalf("expected the narrower firewall declared first to be silent, got %v", orderedWarnings)
    }
}

/* a request path is matched decoded, so a declared path holding a valid escape other than %2F claims only a resource whose name carries the percent sign */
func TestBuilder_BootWarningsNameADeclaredPathThatCarriesAPercentEscape(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(security.NewAccessControl(security.NewAccessControlExactRule("/caf%C3%A9", "ROLE_GLOBAL")), nil, nil, nil, nil)

    builder.AddStatelessFirewall(
        "files",
        security.NewPathPrefixMatcher("/files%41"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration().WithInheritGlobalAccessControl(false).WithAccessControl(security.NewAccessControl(
            security.NewAccessControlExactRule("/files%41/a%20b", "ROLE_LOCAL"),
            security.NewAccessControlExactRule("/files%41/a%2Fb", "ROLE_LOCAL"),
            security.NewAccessControlExactRule("/files%41/100%", "ROLE_LOCAL"),
            security.NewAccessControlRegexRule("^/files%41/x$", "ROLE_LOCAL"),
        )),
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

func TestCarriesPercentEscapeOtherThanSeparator(t *testing.T) {
    for declaredPath, expected := range map[string]bool{
        "/plain":     false,
        "/a%2Fb":     false,
        "/a%2fb":     false,
        "/100%":      false,
        "/a%zzb":     false,
        "/a%20b":     true,
        "/a%2Fb%41":  true,
        "/caf%C3%A9": true,
    } {
        if expected != carriesPercentEscapeOtherThanSeparator(declaredPath) {
            t.Fatalf("expected %q to be %v", declaredPath, expected)
        }
    }
}
