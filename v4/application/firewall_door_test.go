package application

import (
    "context"
    nethttp "net/http"
    "os"
    "os/exec"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security"
    securityconfig "github.com/precision-soft/melody/v4/security/config"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type firewallDoorTestLoginHandler struct{}

func (instance *firewallDoorTestLoginHandler) Login(
    runtimeInstance runtimecontract.Runtime,
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
            router.HandleWithOptions(pattern, bootCollisionTestHandler(), http.RouteOptions{Name: "colliding", Methods: []string{nethttp.MethodGet}})
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
    router.HandleWithOptions("/login", bootCollisionTestHandler(), http.RouteOptions{Name: "page", Methods: []string{nethttp.MethodGet}})
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
    builder.SetGlobal(securityconfig.GlobalConfig{AccessControl: security.NewAccessControl(nil)})
    builder.AddFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        security.NewResolverTokenSource(func(request httpcontract.Request) securitycontract.Token {
            return security.NewAnonymousToken()
        }),
        securityconfig.FirewallConfig{Login: securityconfig.LoginConfig{Path: "/login/check", Name: "door.login", Handler: &firewallDoorTestLoginHandler{}}},
    )
}

func (instance *firewallDoorTestModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    if "" != instance.pattern {
        kernelInstance.HttpRouter().HandleWithOptions(instance.pattern, bootCollisionTestHandler(), http.RouteOptions{Name: "colliding", Methods: []string{nethttp.MethodGet}})
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
    application.RegisterHttpMiddlewareFactories([]applicationcontract.MiddlewareFactory{
        func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
            kernelInstance.HttpRouter().HandleWithOptions("/login/:step", bootCollisionTestHandler(), http.RouteOptions{Name: "late", Methods: []string{nethttp.MethodGet}})

            return func(next httpcontract.Handler) httpcontract.Handler {
                return next
            }
        },
    })

    kernelInstance := application.Boot()

    defer func() {
        if _, isRefusal := recover().(*exception.Error); false == isRefusal {
            t.Fatal("expected the route a factory registered refused")
        }
    }()

    application.installHttpMiddlewares(kernelInstance.HttpKernel())
}
