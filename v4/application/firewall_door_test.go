package application

import (
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security"
    securityconfig "github.com/precision-soft/melody/v4/security/config"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
)

type firewallDoorTestLoginHandler struct{}

func (instance *firewallDoorTestLoginHandler) Login(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    return &securitycontract.LoginResult{Token: input.Token}, nil
}

func newFirewallDoorTestApplication(t *testing.T) (*Application, *http.Router) {
    t.Helper()

    application := newCollisionTestApplication(t)

    router := http.NewRouter()
    application.kernel.(*testKernel).httpRouter = router
    application.securityConfiguration = security.NewCompiledConfiguration(
        []*security.CompiledFirewall{
            security.NewCompiledFirewall(security.CompiledFirewallConfig{
                Name:         "main",
                Matcher:      security.NewPathPrefixMatcher("/"),
                LoginPath:    "/login/check/",
                LoginName:    "door.login",
                LoginHandler: &firewallDoorTestLoginHandler{},
            }),
        },
        nil,
    )

    return application, router
}

func TestFirewallDoor_IsPublishedAsANamedDefinition(t *testing.T) {
    application, router := newFirewallDoorTestApplication(t)

    application.declareFirewallDoorRoutes()

    definition, exists := router.RouteRegistry().RouteDefinition("door.login")
    if false == exists || 1 != len(definition.Methods()) || nethttp.MethodPost != definition.Methods()[0] {
        t.Fatalf("expected the door's definition with its default method, got %v", definition)
    }
}

func TestFirewallDoor_ItsOwnDefinitionDoesNotCollide(t *testing.T) {
    application, _ := newFirewallDoorTestApplication(t)

    application.declareFirewallDoorRoutes()
    application.recordFirewallDoorRouteCollisions()

    if 0 != len(application.bootCollisions) {
        t.Fatalf("expected no collision, got %+v", application.bootCollisions)
    }
}

func TestFirewallDoor_AnyRouteThatClaimsItsPathIsReported(t *testing.T) {
    for name, pattern := range map[string]string{
        "literal":   "/login/check",
        "parameter": "/login/:step",
        "catch all": "/*rest...",
    } {
        t.Run(name, func(t *testing.T) {
            application, router := newFirewallDoorTestApplication(t)

            application.declareFirewallDoorRoutes()
            router.Handle(pattern, bootCollisionTestHandler(), http.RouteOptions{Name: "colliding", Methods: []string{nethttp.MethodGet}})
            application.recordFirewallDoorRouteCollisions()

            if 1 != len(application.bootCollisions) || bootCollisionKindFirewallDoorRoute != application.bootCollisions[0].kind {
                t.Fatalf("expected one door collision, got %+v", application.bootCollisions)
            }

            if false == strings.Contains(application.bootCollisions[0].name, `"colliding"`) || false == strings.Contains(application.bootCollisions[0].name, `"door.login"`) {
                t.Fatalf("expected the route and the door named, got %q", application.bootCollisions[0].name)
            }
        })
    }
}

func TestFirewallDoor_ARouteOnAnotherPathIsAdmitted(t *testing.T) {
    application, router := newFirewallDoorTestApplication(t)

    application.declareFirewallDoorRoutes()
    router.Handle("/login", bootCollisionTestHandler(), http.RouteOptions{Name: "page", Methods: []string{nethttp.MethodGet}})
    application.recordFirewallDoorRouteCollisions()

    if 0 != len(application.bootCollisions) {
        t.Fatalf("expected the page admitted, got %+v", application.bootCollisions)
    }
}

const firewallDoorBootChildVariable = "MELODY_TEST_FIREWALL_DOOR_BOOT_CHILD"

type firewallDoorTestModule struct {
    fakeModule
    pattern string
}

func (instance *firewallDoorTestModule) RegisterSecurity(builder *securityconfig.Builder) {
    builder.SetGlobal(securityconfig.GlobalConfig{AccessControl: openAccessControl()})
    builder.AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        security.NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return security.NewAnonymousToken(), nil
        }, security.ResolverTokenSourceConfig{}),
        securityconfig.FirewallConfig{Login: securityconfig.LoginConfig{Path: "/login/check", Name: "door.login", Handler: &firewallDoorTestLoginHandler{}, LimiterDisabled: true}},
    )
}

func (instance *firewallDoorTestModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    if "" != instance.pattern {
        kernelInstance.HttpRouter().Handle(instance.pattern, bootCollisionTestHandler(), http.RouteOptions{Name: "colliding", Methods: []string{nethttp.MethodGet}})
    }
}

func TestBoot_PublishesADeclaredDoor(t *testing.T) {
    application := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
    application.RegisterModule(&firewallDoorTestModule{fakeModule: fakeModule{name: "door"}})

    kernelInstance := application.Boot()

    for _, definition := range kernelInstance.HttpRouter().RouteDefinitions() {
        if "door.login" == definition.Name() {
            return
        }
    }

    t.Fatal("expected the door's definition published by the boot")
}

/* Boot reports a refusal and exits the process, so the boot runs in a child of the test binary */
func TestBoot_RefusesARouteOnADoorsPath(t *testing.T) {
    if "1" == os.Getenv(firewallDoorBootChildVariable) {
        application := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
        application.RegisterModule(&firewallDoorTestModule{fakeModule: fakeModule{name: "door"}, pattern: "/login/:step"})
        application.Boot()

        return
    }

    command := exec.Command(os.Args[0], "-test.run=^TestBoot_RefusesARouteOnADoorsPath$")
    command.Env = append(os.Environ(), firewallDoorBootChildVariable+"=1")

    output, runErr := command.CombinedOutput()

    if nil == runErr || false == strings.Contains(string(output), "duplicate registrations detected at boot") {
        t.Fatalf("expected the boot refused in the collision report, got %v: %s", runErr, output[max(0, len(output)-400):])
    }
}

func TestInstallHttpMiddlewares_RefusesARouteAFactoryPutOnADoorsPath(t *testing.T) {
    application := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
    application.RegisterModule(&firewallDoorTestModule{fakeModule: fakeModule{name: "door"}})
    application.RegisterHttpMiddlewareFactories([]middlewarepipeline.MiddlewareFactory{
        func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
            kernelInstance.HttpRouter().Handle("/login/:step", bootCollisionTestHandler(), http.RouteOptions{Name: "late", Methods: []string{nethttp.MethodGet}})

            return func(next httpcontract.Handler) httpcontract.Handler {
                return next
            }, nil
        },
    })

    application.Boot()

    defer func() {
        if _, isRefusal := recover().(*exception.Error); false == isRefusal {
            t.Fatal("expected the route a factory registered refused")
        }
    }()

    application.httpKernel = nil
    application.installHttpMiddlewares()
}

func TestFirewallDoor_ARouteOnTheCsrfTokenPathIsReported(t *testing.T) {
    application, router := newFirewallDoorTestApplication(t)
    application.securityConfiguration = security.NewCompiledConfiguration(
        []*security.CompiledFirewall{
            security.NewCompiledFirewall(security.CompiledFirewallConfig{
                Name:          "main",
                Matcher:       security.NewPathPrefixMatcher("/"),
                CsrfTokenPath: "/csrf-token",
                CsrfTokenName: "door.csrfToken",
            }),
        },
        nil,
    )

    application.declareFirewallDoorRoutes()
    router.Handle("/csrf-token", bootCollisionTestHandler(), http.RouteOptions{Name: "colliding", Methods: []string{nethttp.MethodPost}})
    application.recordFirewallDoorRouteCollisions()

    if 1 != len(application.bootCollisions) || false == strings.Contains(application.bootCollisions[0].name, `"door.csrfToken"`) {
        t.Fatalf("expected the route on the token path reported against the token door, got %+v", application.bootCollisions)
    }
}

type csrfApplicationTestModule struct {
    fakeModule
    csrf security.CsrfConfig
}

func (instance *csrfApplicationTestModule) RegisterSecurity(builder *securityconfig.Builder) {
    builder.SetGlobal(securityconfig.GlobalConfig{AccessControl: openAccessControl(), Csrf: instance.csrf})
    builder.AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        security.NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return security.NewAnonymousToken(), nil
        }, security.ResolverTokenSourceConfig{}),
        securityconfig.FirewallConfig{},
    )
}

func (instance *csrfApplicationTestModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    kernelInstance.HttpRouter().Handle("/account", bootCollisionTestHandler(), http.RouteOptions{Name: "account", Methods: []string{nethttp.MethodPost}})
}

func serveCsrfApplicationWrite(t *testing.T, csrf security.CsrfConfig, headers map[string]string) int {
    t.Helper()

    application := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
    application.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})
    application.RegisterModule(&csrfApplicationTestModule{fakeModule: fakeModule{name: "csrf"}, csrf: csrf})
    serviceContainer := application.Boot().ServiceContainer()

    manager := session.SessionMustFromResolver(t.Context(), serviceContainer)
    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    sessionInstance.Set("identity", "u1")
    if saveErr := manager.SaveSession(sessionRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    request := httptest.NewRequest(nethttp.MethodPost, "http://app.example.com/account", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()})
    for name, value := range headers {
        request.Header.Set(name, value)
    }

    return serveTestRequest(t, application, request).Code
}

func TestApplication_SiblingSubdomainWrite_OnASessionRoute_Refused403(t *testing.T) {
    sibling := map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://evil.example.com"}

    if status := serveCsrfApplicationWrite(t, security.CsrfConfig{}, sibling); nethttp.StatusForbidden != status {
        t.Fatalf("expected a sibling subdomain's write refused 403, got %d", status)
    }

    if status := serveCsrfApplicationWrite(t, security.CsrfConfig{}, map[string]string{"Sec-Fetch-Site": "same-origin"}); nethttp.StatusForbidden == status {
        t.Fatalf("expected a same-origin write served, got %d", status)
    }
}

func TestApplication_GlobalCsrfDisabled_ForgedRequestServed(t *testing.T) {
    if status := serveCsrfApplicationWrite(t, security.CsrfConfig{Mode: security.CsrfModeDisabled}, map[string]string{"Sec-Fetch-Site": "cross-site"}); nethttp.StatusForbidden == status {
        t.Fatalf("expected the global door to serve a forged write, got %d", status)
    }
}

func forgedAccountWrite(t *testing.T, applicationInstance *Application, accept string) *httptest.ResponseRecorder {
    t.Helper()

    request := httptest.NewRequest(nethttp.MethodPost, "http://app.example.com/account", nil)
    request.AddCookie(sessionCookieOfAnIdentity(t, applicationInstance.kernel))
    request.Header.Set("Sec-Fetch-Site", "cross-site")
    request.Header.Set("Accept", accept)

    return serveTestRequest(t, applicationInstance, request)
}

func TestApplication_ForgedRequest_OneWarningRecord_RenderedByAccept(t *testing.T) {
    var logger *levelCapturingLogger
    applicationInstance := bootServingTestApplication(t, config.EnvironmentProduction, ApplicationConfig{}, func(applicationInstance *Application) {
        logger = registerLevelCapturingLogger(applicationInstance)
        applicationInstance.RegisterModule(&csrfApplicationTestModule{fakeModule: fakeModule{name: "csrf"}})
    })

    for _, testCase := range []struct {
        accept      string
        contentType string
    }{
        {"application/json", "application/json"},
        {"text/html", "text/html"},
    } {
        from := logger.recorded()

        recorder := forgedAccountWrite(t, applicationInstance, testCase.accept)
        if nethttp.StatusForbidden != recorder.Code || false == strings.HasPrefix(recorder.Result().Header.Get("Content-Type"), testCase.contentType) {
            t.Fatalf("%s: expected a 403 rendered as %s, got %d %q", testCase.accept, testCase.contentType, recorder.Code, recorder.Result().Header.Get("Content-Type"))
        }

        if warnings, errorRecords := logger.countSince(from, loggingcontract.LevelWarning), logger.countSince(from, loggingcontract.LevelError); 1 != warnings || 0 != errorRecords {
            t.Fatalf("%s: expected one warning record and no error, got %d warnings and %d errors", testCase.accept, warnings, errorRecords)
        }
    }
}

func TestApplication_ForgedRequest_SameStatusInDevAndProd(t *testing.T) {
    for _, environmentName := range []string{config.EnvironmentDevelopment, config.EnvironmentProduction} {
        applicationInstance := bootServingTestApplication(t, environmentName, ApplicationConfig{}, func(applicationInstance *Application) {
            applicationInstance.RegisterModule(&csrfApplicationTestModule{fakeModule: fakeModule{name: "csrf"}})
        })

        for _, accept := range []string{"application/json", "text/html"} {
            if recorder := forgedAccountWrite(t, applicationInstance, accept); nethttp.StatusForbidden != recorder.Code {
                t.Fatalf("%s %s: expected 403, got %d", environmentName, accept, recorder.Code)
            }
        }
    }
}
