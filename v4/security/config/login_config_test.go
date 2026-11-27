package config

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    httpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/security"
)

type unclaimingMatcher struct{}

func (instance *unclaimingMatcher) Matches(request httpcontract.Request) bool { return true }

func TestBuilder_AddFirewall_RefusesAMalformedDoorNamingTheField(t *testing.T) {
    var typedNilLoginHandler *noopLoginHandler
    var typedNilLogoutHandler *noopLogoutHandler
    var typedNilAuthenticator *noopAuthenticator

    handler := &noopLoginHandler{}

    for _, testCase := range []struct {
        config FirewallConfig
        field  string
    }{
        {FirewallConfig{Login: LoginConfig{Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Handler: typedNilLoginHandler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Name: "door"}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", LimiterDisabled: true}}, "FirewallConfig.Login.Handler"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: typedNilLoginHandler, LimiterDisabled: true}}, "FirewallConfig.Login.Handler"},
        {FirewallConfig{Logout: LogoutConfig{Path: "/logout"}}, "FirewallConfig.Logout.Handler"},
        {FirewallConfig{Logout: LogoutConfig{Path: "/logout", Handler: typedNilLogoutHandler}}, "FirewallConfig.Logout.Handler"},
        {FirewallConfig{Logout: LogoutConfig{TargetPath: "/"}}, "FirewallConfig.Logout.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "login", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "//login", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/a/../login", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login/:step", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/*rest...", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/log%20in", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login ", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login\u00a0", Handler: handler, LimiterDisabled: true}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Attributes: map[string]any{"_zone": "x"}, LimiterDisabled: true}}, "FirewallConfig.Login.Attributes"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Authenticator: typedNilAuthenticator, LimiterDisabled: true}}, "FirewallConfig.Login.Authenticator"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"GET"}, LimiterDisabled: true}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"post", "head"}, LimiterDisabled: true}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"options"}, LimiterDisabled: true}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"TRACE"}, LimiterDisabled: true}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{""}, LimiterDisabled: true}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, TargetPath: "https://elsewhere.example/", LimiterDisabled: true}}, "FirewallConfig.Login.TargetPath"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, TargetPath: "//elsewhere.example/", LimiterDisabled: true}}, "FirewallConfig.Login.TargetPath"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, TargetPath: "relative", LimiterDisabled: true}}, "FirewallConfig.Login.TargetPath"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, LimiterConfig: httpmiddleware.RateLimitConfig{KeyExtractor: func(httpcontract.Request) string { return "" }}}}, "FirewallConfig.Login.Limiter"},
        {FirewallConfig{Login: LoginConfig{Path: "/door/", Handler: handler, LimiterDisabled: true}, Logout: LogoutConfig{Path: "/door", Handler: &noopLogoutHandler{}}}, "FirewallConfig.Logout.Path"},
    } {
        t.Run(testCase.field, func(t *testing.T) {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || testCase.field != refusal.Context()["field"] {
                    t.Fatalf("expected a refusal naming %s, got %v", testCase.field, refusal)
                }
            }()

            NewBuilder().AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, testCase.config)
        })
    }
}

func TestBuilder_AddFirewall_RefusesALoginDoorWithoutALimiterUnlessDisabled(t *testing.T) {
    handler := &noopLoginHandler{}
    keyExtractor := func(httpcontract.Request) string { return "" }

    for _, testCase := range []struct {
        name    string
        login   LoginConfig
        field   string
        message string
    }{
        {"no limiter", LoginConfig{Path: "/login", Handler: handler}, "FirewallConfig.Login.Limiter", "a security firewall login door declares a limiter, so credential guessing is bounded; declare FirewallConfig.Login.LimiterDisabled when a limiter in front of the application meters it"},
        {"disabled beside a limiter", LoginConfig{Path: "/login", Handler: handler, Limiter: &allowingRateLimiter{}, LimiterDisabled: true}, "FirewallConfig.Login.LimiterDisabled", "a security firewall door declared unmetered may not declare a limiter"},
        {"disabled beside a limiter config", LoginConfig{Path: "/login", Handler: handler, LimiterConfig: httpmiddleware.RateLimitConfig{KeyExtractor: keyExtractor}, LimiterDisabled: true}, "FirewallConfig.Login.LimiterDisabled", "a security firewall door declared unmetered may not declare a limiter"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || testCase.field != refusal.Context()["field"] || testCase.message != refusal.Message() || "/login" != refusal.Context()["value"] {
                    t.Fatalf("expected the refusal %q naming %s, got %v", testCase.message, testCase.field, refusal)
                }
            }()

            NewBuilder().AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Login: testCase.login})
        })
    }
}

func TestCompile_LoginDoorLimiterAndItsDoorBothCompile(t *testing.T) {
    for _, testCase := range []struct {
        name  string
        login LoginConfig
    }{
        {"metered", LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, Limiter: &allowingRateLimiter{}}},
        {"unmetered on purpose", LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, LimiterDisabled: true}},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            builder := NewBuilder()
            builder.SetGlobal(GlobalConfig{AccessControl: NewAccessControlBuilder().AllowAnonymous("/").Build()})
            builder.AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Login: testCase.login})

            if doors := builder.BuildAndCompile().Firewalls()[0].Doors(); 1 != len(doors) || "/login" != doors[0].Path {
                t.Fatalf("expected the login door compiled, got %+v", doors)
            }
        })
    }
}

func TestCompile_ResolvesTheDoorDefaults(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: openAccessControl()})
    builder.AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        FirewallConfig{
            Login:  LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, LimiterDisabled: true},
            Logout: LogoutConfig{Path: "/logout", Handler: &noopLogoutHandler{}, Name: "app.logout", Methods: []string{"delete"}},
        },
    )

    doors := builder.BuildAndCompile().Firewalls()[0].Doors()
    if 2 != len(doors) {
        t.Fatalf("expected both doors, got %d", len(doors))
    }

    if "security.main.login" != doors[0].Name || 1 != len(doors[0].Methods) || "POST" != doors[0].Methods[0] {
        t.Fatalf("expected the login door's defaults, got %+v", doors[0])
    }

    if "app.logout" != doors[1].Name || 1 != len(doors[1].Methods) || "DELETE" != doors[1].Methods[0] {
        t.Fatalf("expected the logout door's declared name and upper-cased method, got %+v", doors[1])
    }
}

func TestCompile_RefusesADoorOutsideItsOwnPrefix(t *testing.T) {
    _, err := Compile(Configuration{
        global: GlobalConfig{AccessControl: openAccessControl()},
        firewalls: []firewallDeclaration{
            {
                name:        "admin",
                matcher:     security.NewSegmentPrefixMatcher("/admin"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Login: LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, LimiterDisabled: true}},
            },
        },
    })
    if nil == err {
        t.Fatal("expected the unreachable door refused")
    }
}

func TestCompile_RefusesADoorAnEarlierFirewallClaims(t *testing.T) {
    _, err := Compile(Configuration{
        global: GlobalConfig{AccessControl: openAccessControl()},
        firewalls: []firewallDeclaration{
            {name: "first", matcher: security.NewPathPrefixMatcher("/login"), tokenSource: &anonymousTokenSource{}},
            {
                name:        "main",
                matcher:     security.NewPathPrefixMatcher("/"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Login: LoginConfig{Path: "/login/check", Handler: &noopLoginHandler{}, LimiterDisabled: true}},
            },
        },
    })
    if nil == err {
        t.Fatal("expected the shadowed door refused")
    }
}

func TestCompile_AdmitsADoorInsideItsPrefixBehindAnUnrelatedFirewall(t *testing.T) {
    _, err := Compile(Configuration{
        global: GlobalConfig{AccessControl: openAccessControl()},
        firewalls: []firewallDeclaration{
            {name: "internal", matcher: security.NewSegmentPrefixMatcher("/internal"), tokenSource: &anonymousTokenSource{}},
            {
                name:        "main",
                matcher:     security.NewPathPrefixMatcher("/"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Login: LoginConfig{Path: "/login/check/", Handler: &noopLoginHandler{}, LimiterDisabled: true}},
            },
        },
    })
    if nil != err {
        t.Fatalf("unexpected refusal: %v", err)
    }
}

func TestBootWarnings_NameADoorWhoseReachAMatcherCannotState(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: openAccessControl()})
    builder.AddFirewall("main", &unclaimingMatcher{}, &anonymousTokenSource{}, FirewallConfig{Login: LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, LimiterDisabled: true}})
    builder.BuildAndCompile()

    for _, warning := range builder.BootWarnings() {
        if internal.BootWarningNameFirewallDoorReachUnknown == warning.Name {
            return
        }
    }

    t.Fatalf("expected the door reach warning, got %+v", builder.BootWarnings())
}

func TestBootWarnings_StaySilentOverPrefixMatchers(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: openAccessControl()})
    builder.AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Login: LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, LimiterDisabled: true}})
    builder.BuildAndCompile()

    for _, warning := range builder.BootWarnings() {
        if internal.BootWarningNameFirewallDoorReachUnknown == warning.Name {
            t.Fatalf("expected no door reach warning, got %+v", warning)
        }
    }
}

func TestRefuseUnreachableFirewallDoors_TokenPathOutsideThePrefix_Refused(t *testing.T) {
    builder := NewBuilder()
    builder.AddFirewall("main", security.NewPathPrefixMatcher("/app"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{AccessControl: openAccessControl()}, Csrf: FirewallCsrfConfig{TokenPath: "/csrf-token"}})

    expectRefusalNamingField(t, "a security firewall door lies outside its firewall's prefix, so no request reaches it", "security.main.csrfToken", func() { builder.BuildAndCompile() })
}

func TestBootWarnings_Csrf_LoginFirewallDisabled_Named(t *testing.T) {
    for declaredBy, testCase := range map[string]struct {
        global security.CsrfConfig
        local  security.CsrfMode
    }{
        "firewall": {security.CsrfConfig{}, security.CsrfModeDisabled},
        "global":   {security.CsrfConfig{Mode: security.CsrfModeDisabled}, ""},
    } {
        builder := NewBuilder()
        builder.SetGlobal(GlobalConfig{Csrf: testCase.global})
        builder.AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, csrfLoginFirewallConfig(FirewallCsrfConfig{Mode: testCase.local}))
        builder.BuildAndCompile()

        found := false
        for _, warning := range builder.BootWarnings() {
            if internal.BootWarningNameCsrfDisabledOnLoginFirewall == warning.Name && "main" == warning.Context["firewall"] && declaredBy == warning.Context["declaredBy"] {
                found = true
            }
        }

        if false == found {
            t.Fatalf("%s: expected the warning naming the firewall, got %+v", declaredBy, builder.BootWarnings())
        }
    }
}

func TestBootWarnings_Csrf_Protected_None(t *testing.T) {
    builder := NewBuilder()
    builder.AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, csrfLoginFirewallConfig(FirewallCsrfConfig{}))
    builder.AddFirewall("api", security.NewPathPrefixMatcher("/api"), &anonymousTokenSource{}, FirewallConfig{Override: FirewallOverride{AccessControl: openAccessControl()}, Csrf: FirewallCsrfConfig{Mode: security.CsrfModeDisabled}})

    defer func() { _ = recover() }()
    builder.BuildAndCompile()

    for _, warning := range builder.BootWarnings() {
        if internal.BootWarningNameCsrfDisabledOnLoginFirewall == warning.Name {
            t.Fatalf("expected no csrf warning without a disabled sign-in firewall, got %+v", warning)
        }
    }
}

func TestLoginConfig_CsrfDisabledAlone_CountsAsADeclaredDoor(t *testing.T) {
    expectRefusalNamingField(t, "a security firewall door declares a path", "FirewallConfig.Login.Path", func() {
        NewBuilder().AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Login: LoginConfig{CsrfDisabled: true}})
    })

    expectRefusalNamingField(t, "a security firewall door declares a path", "FirewallConfig.Logout.Path", func() {
        NewBuilder().AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Logout: LogoutConfig{CsrfDisabled: true}})
    })
}
