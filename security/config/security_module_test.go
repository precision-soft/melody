package config

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/security"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

type anonymousTokenSource struct{}

func (instance *anonymousTokenSource) Name() string {
    return "anonymous"
}

func (instance *anonymousTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    _ = runtimeInstance
    _ = request

    return security.NewAnonymousToken(), nil
}

var _ securitycontract.TokenSource = (*anonymousTokenSource)(nil)

type noopLoginHandler struct{}

func (instance *noopLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    _ = runtimeInstance
    _ = request
    _ = input

    return &securitycontract.LoginResult{Token: security.NewAnonymousToken(), Response: nil}, nil
}

var _ securitycontract.LoginHandler = (*noopLoginHandler)(nil)

type noopLogoutHandler struct{}

func (instance *noopLogoutHandler) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    _ = runtimeInstance
    _ = request
    _ = input

    return &securitycontract.LogoutResult{Response: nil}, nil
}

var _ securitycontract.LogoutHandler = (*noopLogoutHandler)(nil)

func TestBuilder_AddStatelessFirewall_CompilesWithoutLoginLogout(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    )

    compiledConfiguration := builder.BuildAndCompile()
    if nil == compiledConfiguration {
        t.Fatalf("expected compiled configuration")
    }

    firewalls := compiledConfiguration.Firewalls()
    if 1 != len(firewalls) {
        t.Fatalf("expected 1 firewall, got %d", len(firewalls))
    }

    if "" != firewalls[0].LoginPath() {
        t.Fatalf("expected empty login path")
    }

    if "" != firewalls[0].LogoutPath() {
        t.Fatalf("expected empty logout path")
    }
}

func TestBuilder_AddFirewall_WithStatelessOverride_CompilesWithoutLoginLogout(t *testing.T) {
    builder := NewBuilder()

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        "",
        "",
        nil,
        nil,
        NewFirewallOverrideConfiguration().WithStateless(true),
    )

    compiledConfiguration := builder.BuildAndCompile()
    if nil == compiledConfiguration {
        t.Fatalf("expected compiled configuration")
    }
}

func TestBuilder_AddFirewall_WithStatelessOverride_PanicsWhenLoginLogoutProvided(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    builder := NewBuilder()

    builder.AddFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        "/login",
        "/logout",
        &noopLoginHandler{},
        &noopLogoutHandler{},
        NewFirewallOverrideConfiguration().WithStateless(true),
    )
}

func TestBuilder_RoleHierarchy_AutoUpgradesRoleVoterToRoleHierarchyVoter(t *testing.T) {
    builder := NewBuilder()

    roleHierarchy := security.NewRoleHierarchy(map[string][]string{
        "ROLE_ADMIN": {"ROLE_USER"},
    })

    builder.SetGlobal(
        nil,
        roleHierarchy,
        security.NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            security.NewRoleVoter(),
        ),
        nil,
        nil,
    )

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/api"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected compiled configuration")
    }

    firewalls := compiled.Firewalls()
    if 1 != len(firewalls) {
        t.Fatalf("expected 1 firewall, got %d", len(firewalls))
    }

    dm := firewalls[0].AccessDecisionManager()
    if nil == dm {
        t.Fatalf("expected access decision manager")
    }

    adminToken := &testRoleToken{roles: []string{"ROLE_ADMIN"}}

    err := dm.DecideAll(adminToken, []string{"ROLE_USER"}, nil)
    if nil != err {
        t.Fatalf("expected ROLE_ADMIN to be granted ROLE_USER via hierarchy, got: %v", err)
    }

    userToken := &testRoleToken{roles: []string{"ROLE_USER"}}

    err = dm.DecideAll(userToken, []string{"ROLE_ADMIN"}, nil)
    if nil == err {
        t.Fatalf("expected ROLE_USER to be denied ROLE_ADMIN")
    }
}

/* @info the zero value of the exported override struct must inherit the global access control the way the constructor does; without the default a firewall added with FirewallOverrideConfiguration{} compiles an empty access control that never falls back to the global policy and opens every route behind it */
func TestBuilder_ZeroValueOverrideInheritsGlobalAccessControl(t *testing.T) {
    builder := NewBuilder()

    builder.SetGlobal(
        security.NewAccessControl(security.NewAccessControlRule("/admin", "ROLE_ADMIN")),
        nil,
        security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, security.NewRoleVoter()),
        nil,
        nil,
    )

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        nil,
        &anonymousTokenSource{},
        FirewallOverrideConfiguration{},
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected compiled configuration")
    }

    attributes, matched := compiled.Firewalls()[0].AccessControl().Match("/admin")
    if false == matched {
        t.Fatalf("expected the firewall to inherit the global rule for /admin")
    }

    if 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected inherited ROLE_ADMIN, got %v", attributes)
    }
}

/* @info a global access control declared without any firewall must still compile into an enforcing configuration; dropping it left every global rule silently unenforced */
func TestBuilder_GlobalAccessControlWithoutFirewallStillCompiles(t *testing.T) {
    builder := NewBuilder()

    builder.SetGlobal(
        security.NewAccessControl(security.NewAccessControlRule("/admin", "ROLE_ADMIN")),
        nil,
        security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, security.NewRoleVoter()),
        nil,
        nil,
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected an enforcing configuration for a global-only policy")
    }

    attributes, matched := compiled.GlobalAccessControl().Match("/admin")
    if false == matched || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected the global rule for /admin to survive, got matched=%v attributes=%v", matched, attributes)
    }
}

func TestBuilder_NoFirewallAndNoGlobalCompilesToNil(t *testing.T) {
    builder := NewBuilder()

    if nil != builder.BuildAndCompile() {
        t.Fatalf("expected nil when neither firewall nor global policy is declared")
    }
}

type testRoleToken struct {
    roles []string
}

func (instance *testRoleToken) UserIdentifier() string { return "test" }

func (instance *testRoleToken) Roles() []string { return instance.roles }

func (instance *testRoleToken) IsAuthenticated() bool { return true }

/* newValidFirewallArguments spells the smallest stateful firewall that passes validation, so each refusal test can spoil exactly one argument and nothing else. */
func newValidFirewallArguments() (string, securitycontract.Matcher, []securitycontract.Rule, securitycontract.TokenSource, string, string, securitycontract.LoginHandler, securitycontract.LogoutHandler, FirewallOverrideConfiguration) {
    return "main",
        security.NewPathPrefixMatcher("/"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        "/login",
        "/logout",
        &noopLoginHandler{},
        &noopLogoutHandler{},
        NewFirewallOverrideConfiguration()
}

/* @info every one of these is a boot-time refusal, and each is pinned by MESSAGE: the eight of them panic from the same helper, so a value-blind assertion would pass no matter which guard fired — including the wrong one */
func TestBuilder_ValidateFirewall_RefusesEachMissingPiece(t *testing.T) {
    refusalList := []struct {
        name            string
        expectedMessage string
        spoil           func(*string, *securitycontract.Matcher, *securitycontract.TokenSource, *string, *string, *securitycontract.LoginHandler, *securitycontract.LogoutHandler)
    }{
        {
            name:            "empty name",
            expectedMessage: "security firewall name may not be empty",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *name = ""
            },
        },
        {
            name:            "nil matcher",
            expectedMessage: "security firewall matcher is nil",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *matcher = nil
            },
        },
        {
            name:            "nil token source",
            expectedMessage: "security firewall token source is nil",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *tokenSource = nil
            },
        },
        {
            name:            "empty login path",
            expectedMessage: "security firewall login path may not be empty",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *loginPath = ""
            },
        },
        {
            name:            "empty logout path",
            expectedMessage: "security firewall logout path may not be empty",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *logoutPath = ""
            },
        },
        {
            name:            "nil login handler",
            expectedMessage: "security firewall login handler is nil",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *loginHandler = nil
            },
        },
        {
            name:            "nil logout handler",
            expectedMessage: "security firewall logout handler is nil",
            spoil: func(name *string, matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginPath *string, logoutPath *string, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *logoutHandler = nil
            },
        },
    }

    for _, refusal := range refusalList {
        t.Run(refusal.name, func(t *testing.T) {
            name, matcher, rules, tokenSource, loginPath, logoutPath, loginHandler, logoutHandler, override := newValidFirewallArguments()

            refusal.spoil(&name, &matcher, &tokenSource, &loginPath, &logoutPath, &loginHandler, &logoutHandler)

            testhelper.AssertPanicsWithError(
                t,
                func() {
                    NewBuilder().AddFirewall(
                        name,
                        matcher,
                        rules,
                        tokenSource,
                        loginPath,
                        logoutPath,
                        loginHandler,
                        logoutHandler,
                        override,
                    )
                },
                refusal.expectedMessage,
            )
        })
    }
}

/* @info the refused firewall names itself in the error context, which is the difference between a readable boot failure and hunting through a wiring file */
func TestBuilder_ValidateFirewall_NamesTheFirewallInTheRefusal(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the refusal to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        contextValue := exception.LogContext(recoveredErr)
        if "api" != contextValue["firewallName"] {
            t.Fatalf("expected the refused firewall to be named, got %v", contextValue["firewallName"])
        }
    }()

    NewBuilder().AddFirewall(
        "api",
        nil,
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        "/login",
        "/logout",
        &noopLoginHandler{},
        &noopLogoutHandler{},
        NewFirewallOverrideConfiguration(),
    )
}

/* @info a stateless firewall that also declares login configuration is contradictory: each of the four pieces alone triggers the refusal, so none of them can slip through by being weighed with an OR that lost a term */
func TestBuilder_AddStatelessFirewall_RefusesEachLoginPiece(t *testing.T) {
    spoilList := []struct {
        name          string
        loginPath     string
        logoutPath    string
        loginHandler  securitycontract.LoginHandler
        logoutHandler securitycontract.LogoutHandler
    }{
        {name: "login path", loginPath: "/login"},
        {name: "logout path", logoutPath: "/logout"},
        {name: "login handler", loginHandler: &noopLoginHandler{}},
        {name: "logout handler", logoutHandler: &noopLogoutHandler{}},
    }

    for _, spoil := range spoilList {
        t.Run(spoil.name, func(t *testing.T) {
            override := NewFirewallOverrideConfiguration()
            override.stateless = true

            testhelper.AssertPanicsWithError(
                t,
                func() {
                    NewBuilder().AddFirewall(
                        "main",
                        security.NewPathPrefixMatcher("/"),
                        []securitycontract.Rule{},
                        &anonymousTokenSource{},
                        spoil.loginPath,
                        spoil.logoutPath,
                        spoil.loginHandler,
                        spoil.logoutHandler,
                        override,
                    )
                },
                "security stateless firewall may not define login or logout configuration",
            )
        })
    }
}

/* @info the global configuration is declared once: a second call is a wiring mistake that would otherwise overwrite the first policy silently */
func TestBuilder_SetGlobal_RefusesASecondDefinition(t *testing.T) {
    builder := NewBuilder().SetGlobal(nil, nil, nil, nil, nil)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            builder.SetGlobal(nil, nil, nil, nil, nil)
        },
        "security global configuration may only be defined once",
    )
}

/* @info AddStatefulFirewall forces the stateless flag off, so a firewall added through it demands login configuration even when the override it was handed says stateless */
func TestBuilder_AddStatefulFirewall_ForcesTheStatefulShape(t *testing.T) {
    override := NewFirewallOverrideConfiguration()
    override.stateless = true

    compiled := NewBuilder().AddStatefulFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        "/login",
        "/logout",
        &noopLoginHandler{},
        &noopLogoutHandler{},
        override,
    ).BuildAndCompile()

    if nil == compiled {
        t.Fatalf("expected the stateful firewall to compile")
    }

    firewalls := compiled.Firewalls()
    if 1 != len(firewalls) {
        t.Fatalf("expected one firewall, got %d", len(firewalls))
    }

    if "/login" != firewalls[0].LoginPath() {
        t.Fatalf("expected the login path to survive, got %q", firewalls[0].LoginPath())
    }
}

/* @info the same override handed to AddStatefulFirewall would be refused by AddStatelessFirewall: the two constructors decide the shape, not the caller's flag */
func TestBuilder_AddStatelessFirewall_ForcesTheStatelessShape(t *testing.T) {
    override := NewFirewallOverrideConfiguration()
    override.stateless = false

    compiled := NewBuilder().AddStatelessFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        override,
    ).BuildAndCompile()

    if nil == compiled {
        t.Fatalf("expected the stateless firewall to compile")
    }

    if "" != compiled.Firewalls()[0].LoginPath() {
        t.Fatalf("expected no login path on a stateless firewall")
    }
}

/* @info BuildAndCompile turns a compile error into a boot panic rather than handing back a nil configuration the kernel would register nothing from */
func TestBuilder_BuildAndCompile_PanicsOnACompileError(t *testing.T) {
    builder := NewBuilder()

    builder.firewalls = append(
        builder.firewalls,
        FirewallConfiguration{
            name:        "",
            matcher:     security.NewPathPrefixMatcher("/"),
            tokenSource: &anonymousTokenSource{},
        },
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            builder.BuildAndCompile()
        },
        "security firewall name may not be empty",
    )
}

/* @info a typed nil is not `nil ==`: handed to the builder it passed validation, passed Compile, and was called on the request path outside any recovery. Each of the four interface-typed pieces is pinned separately, and by message, so a guard that fires for the wrong reason cannot pass for the right one. */
func TestBuilder_ValidateFirewall_RefusesATypedNilForEachInterfacePiece(t *testing.T) {
    var typedNilMatcher *security.PathPrefixMatcher
    var typedNilTokenSource *anonymousTokenSource
    var typedNilLoginHandler *noopLoginHandler
    var typedNilLogoutHandler *noopLogoutHandler

    refusalList := []struct {
        name            string
        expectedMessage string
        spoil           func(*securitycontract.Matcher, *securitycontract.TokenSource, *securitycontract.LoginHandler, *securitycontract.LogoutHandler)
    }{
        {
            name:            "typed nil matcher",
            expectedMessage: "security firewall matcher is nil",
            spoil: func(matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *matcher = typedNilMatcher
            },
        },
        {
            name:            "typed nil token source",
            expectedMessage: "security firewall token source is nil",
            spoil: func(matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *tokenSource = typedNilTokenSource
            },
        },
        {
            name:            "typed nil login handler",
            expectedMessage: "security firewall login handler is nil",
            spoil: func(matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *loginHandler = typedNilLoginHandler
            },
        },
        {
            name:            "typed nil logout handler",
            expectedMessage: "security firewall logout handler is nil",
            spoil: func(matcher *securitycontract.Matcher, tokenSource *securitycontract.TokenSource, loginHandler *securitycontract.LoginHandler, logoutHandler *securitycontract.LogoutHandler) {
                *logoutHandler = typedNilLogoutHandler
            },
        },
    }

    for _, refusal := range refusalList {
        t.Run(refusal.name, func(t *testing.T) {
            name, matcher, rules, tokenSource, loginPath, logoutPath, loginHandler, logoutHandler, override := newValidFirewallArguments()

            refusal.spoil(&matcher, &tokenSource, &loginHandler, &logoutHandler)

            testhelper.AssertPanicsWithError(
                t,
                func() {
                    NewBuilder().AddFirewall(
                        name,
                        matcher,
                        rules,
                        tokenSource,
                        loginPath,
                        logoutPath,
                        loginHandler,
                        logoutHandler,
                        override,
                    )
                },
                refusal.expectedMessage,
            )
        })
    }
}

/* @info Compile carries the same refusals for a configuration that never went through the builder — the two validation walls must agree, or a typed nil refused at one is admitted at the other */
func TestCompile_RefusesATypedNilForEachInterfacePiece(t *testing.T) {
    var typedNilMatcher *security.PathPrefixMatcher
    var typedNilTokenSource *anonymousTokenSource
    var typedNilLoginHandler *noopLoginHandler
    var typedNilLogoutHandler *noopLogoutHandler

    refusalList := []struct {
        name            string
        expectedMessage string
        firewall        FirewallConfiguration
    }{
        {
            name:            "typed nil matcher",
            expectedMessage: "security firewall matcher is nil",
            firewall: FirewallConfiguration{
                name:    "main",
                matcher: typedNilMatcher,
            },
        },
        {
            name:            "typed nil token source",
            expectedMessage: "security firewall token source is nil",
            firewall: FirewallConfiguration{
                name:        "main",
                matcher:     security.NewPathPrefixMatcher("/"),
                tokenSource: typedNilTokenSource,
            },
        },
        {
            name:            "typed nil login handler",
            expectedMessage: "security firewall login handler is nil",
            firewall: FirewallConfiguration{
                name:         "main",
                matcher:      security.NewPathPrefixMatcher("/"),
                tokenSource:  &anonymousTokenSource{},
                loginPath:    "/login",
                logoutPath:   "/logout",
                loginHandler: typedNilLoginHandler,
            },
        },
        {
            name:            "typed nil logout handler",
            expectedMessage: "security firewall logout handler is nil",
            firewall: FirewallConfiguration{
                name:          "main",
                matcher:       security.NewPathPrefixMatcher("/"),
                tokenSource:   &anonymousTokenSource{},
                loginPath:     "/login",
                logoutPath:    "/logout",
                loginHandler:  &noopLoginHandler{},
                logoutHandler: typedNilLogoutHandler,
            },
        },
    }

    for _, refusal := range refusalList {
        t.Run(refusal.name, func(t *testing.T) {
            compiled, err := Compile(Configuration{
                firewalls: []FirewallConfiguration{refusal.firewall},
            })
            if nil == err {
                t.Fatalf("expected the typed nil to be refused at compile time")
            }

            if nil != compiled {
                t.Fatalf("expected no compiled configuration alongside the refusal")
            }

            if false == strings.Contains(err.Error(), refusal.expectedMessage) {
                t.Fatalf("expected %q, got %q", refusal.expectedMessage, err.Error())
            }
        })
    }
}

/* @info a stateless firewall carrying a TYPED nil login handler is not carrying a handler: read as one it would be refused as a contradictory configuration, which is a refusal for the wrong reason at the wrong place */
func TestBuilder_AddStatelessFirewall_ReadsATypedNilHandlerAsAbsent(t *testing.T) {
    var typedNilLoginHandler *noopLoginHandler

    override := NewFirewallOverrideConfiguration()
    override.stateless = true

    compiled := NewBuilder().AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        []securitycontract.Rule{},
        &anonymousTokenSource{},
        "",
        "",
        typedNilLoginHandler,
        nil,
        override,
    ).BuildAndCompile()

    if nil == compiled {
        t.Fatalf("expected the stateless firewall to compile")
    }
}

/* @info the builder owns the rule list it was handed: a caller that keeps the slice and edits it after registering the firewall must not be able to swap a rule the compiled firewall enforces */
func TestBuilder_AddFirewall_CopiesTheCallersRules(t *testing.T) {
    originalRule := security.NewApiKeyHeaderRule(
        security.NewPathPrefixMatcher("/"),
        "X-Api-Key",
        "expected-secret",
    )

    callerRules := []securitycontract.Rule{originalRule}

    builder := NewBuilder().AddStatelessFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        callerRules,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    )

    callerRules[0] = nil

    compiled := builder.BuildAndCompile()

    rules := compiled.Firewalls()[0].Rules()
    if 1 != len(rules) {
        t.Fatalf("expected one rule, got %d", len(rules))
    }

    if originalRule != rules[0] {
        t.Fatalf("expected the builder to keep its own copy of the rules")
    }
}
