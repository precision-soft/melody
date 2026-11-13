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
        {FirewallConfig{Login: LoginConfig{Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Handler: typedNilLoginHandler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Name: "door"}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login"}}, "FirewallConfig.Login.Handler"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: typedNilLoginHandler}}, "FirewallConfig.Login.Handler"},
        {FirewallConfig{Logout: LogoutConfig{Path: "/logout"}}, "FirewallConfig.Logout.Handler"},
        {FirewallConfig{Logout: LogoutConfig{Path: "/logout", Handler: typedNilLogoutHandler}}, "FirewallConfig.Logout.Handler"},
        {FirewallConfig{Logout: LogoutConfig{TargetPath: "/"}}, "FirewallConfig.Logout.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "login", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "//login", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/a/../login", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login/:step", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/*rest...", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/log%20in", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login ", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login\u00a0", Handler: handler}}, "FirewallConfig.Login.Path"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Attributes: map[string]any{"_zone": "x"}}}, "FirewallConfig.Login.Attributes"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Authenticator: typedNilAuthenticator}}, "FirewallConfig.Login.Authenticator"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"GET"}}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"post", "head"}}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"options"}}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{"TRACE"}}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, Methods: []string{""}}}, "FirewallConfig.Login.Methods"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, TargetPath: "https://elsewhere.example/"}}, "FirewallConfig.Login.TargetPath"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, TargetPath: "//elsewhere.example/"}}, "FirewallConfig.Login.TargetPath"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, TargetPath: "relative"}}, "FirewallConfig.Login.TargetPath"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, CrossSite: "maybe"}}, "FirewallConfig.Login.CrossSite"},
        {FirewallConfig{Login: LoginConfig{Path: "/login", Handler: handler, LimiterConfig: httpmiddleware.RateLimitConfig{KeyExtractor: func(httpcontract.Request) string { return "" }}}}, "FirewallConfig.Login.Limiter"},
        {FirewallConfig{Login: LoginConfig{Path: "/door/", Handler: handler}, Logout: LogoutConfig{Path: "/door", Handler: &noopLogoutHandler{}}}, "FirewallConfig.Logout.Path"},
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

func TestCompile_ResolvesTheDoorDefaults(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    builder.AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        &anonymousTokenSource{},
        FirewallConfig{
            Login:  LoginConfig{Path: "/login", Handler: &noopLoginHandler{}},
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
        global: GlobalConfig{AccessControl: security.NewAccessControl(nil)},
        firewalls: []firewallDeclaration{
            {
                name:        "admin",
                matcher:     security.NewSegmentPrefixMatcher("/admin"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Login: LoginConfig{Path: "/login", Handler: &noopLoginHandler{}}},
            },
        },
    })
    if nil == err {
        t.Fatal("expected the unreachable door refused")
    }
}

func TestCompile_RefusesADoorAnEarlierFirewallClaims(t *testing.T) {
    _, err := Compile(Configuration{
        global: GlobalConfig{AccessControl: security.NewAccessControl(nil)},
        firewalls: []firewallDeclaration{
            {name: "first", matcher: security.NewPathPrefixMatcher("/login"), tokenSource: &anonymousTokenSource{}},
            {
                name:        "main",
                matcher:     security.NewPathPrefixMatcher("/"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Login: LoginConfig{Path: "/login/check", Handler: &noopLoginHandler{}}},
            },
        },
    })
    if nil == err {
        t.Fatal("expected the shadowed door refused")
    }
}

func TestCompile_AdmitsADoorInsideItsPrefixBehindAnUnrelatedFirewall(t *testing.T) {
    _, err := Compile(Configuration{
        global: GlobalConfig{AccessControl: security.NewAccessControl(nil)},
        firewalls: []firewallDeclaration{
            {name: "internal", matcher: security.NewSegmentPrefixMatcher("/internal"), tokenSource: &anonymousTokenSource{}},
            {
                name:        "main",
                matcher:     security.NewPathPrefixMatcher("/"),
                tokenSource: &anonymousTokenSource{},
                config:      FirewallConfig{Login: LoginConfig{Path: "/login/check/", Handler: &noopLoginHandler{}}},
            },
        },
    })
    if nil != err {
        t.Fatalf("unexpected refusal: %v", err)
    }
}

func TestBootWarnings_NameADoorWhoseReachAMatcherCannotState(t *testing.T) {
    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    builder.AddFirewall("main", &unclaimingMatcher{}, &anonymousTokenSource{}, FirewallConfig{Login: LoginConfig{Path: "/login", Handler: &noopLoginHandler{}}})
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
    builder.SetGlobal(GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    builder.AddFirewall("main", security.NewPathPrefixMatcher("/"), &anonymousTokenSource{}, FirewallConfig{Login: LoginConfig{Path: "/login", Handler: &noopLoginHandler{}}})
    builder.BuildAndCompile()

    for _, warning := range builder.BootWarnings() {
        if internal.BootWarningNameFirewallDoorReachUnknown == warning.Name {
            t.Fatalf("expected no door reach warning, got %+v", warning)
        }
    }
}
