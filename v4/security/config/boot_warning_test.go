package config

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
)

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
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{"ROLE_GLOBAL"}})}), AccessDecisionManager: roleDecisionManager()})

    builder.AddFirewall("openOnPurpose", security.NewPathPrefixMatcher("/three"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{GlobalAccessControlDisabled: true, AccessControl: security.NewAccessControl(nil)}})
    builder.AddFirewall("overrideOnlyOpen", security.NewPathPrefixMatcher("/six"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{MergeStrategy: AccessControlMergeOverrideOnly, AccessControl: security.NewAccessControl(nil)}})
    builder.AddFirewall("inherits", security.NewPathPrefixMatcher("/four"), &anonymousTokenSource{}, FirewallConfig{})
    builder.AddFirewall("declaresItsOwn", security.NewPathPrefixMatcher("/five"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{MergeStrategy: AccessControlMergeOverrideOnly, AccessControl: security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewRawPrefixRule("/five", accesscontrol.RuleConfig{Attributes: []string{"ROLE_LOCAL"}})})}})

    if compiled := builder.BuildAndCompile(); 4 != len(compiled.Firewalls()) {
        t.Fatalf("expected the four firewalls compiled, got %d", len(compiled.Firewalls()))
    }

    openGlobalBuilder := NewBuilder()
    openGlobalBuilder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    openGlobalBuilder.AddFirewall("inheritsTheOpenGlobal", security.NewPathPrefixMatcher("/y"), &anonymousTokenSource{}, FirewallConfig{})
    openGlobalBuilder.AddFirewall("declaresOnlyItsOwn", security.NewPathPrefixMatcher("/x"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{AccessControl: security.NewAccessControl(nil)}})

    if compiled := openGlobalBuilder.BuildAndCompile(); 2 != len(compiled.Firewalls()) {
        t.Fatalf("expected the two open firewalls compiled, got %d", len(compiled.Firewalls()))
    }
}
