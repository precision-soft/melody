package config

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/route"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecurityconfig "github.com/precision-soft/melody/v3/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* compiledSecurityModule builds the module up to what RegisterSecurity reads — the internal-auth secrets, the token validators, and the impersonation resolver — and compiles the configuration it registers, so a test asks the rule table the same question the access-control listener asks. */
func compiledSecurityModule(t *testing.T) *melodysecurityconfig.Builder {
    t.Helper()

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    moduleInstance.buildTokenAuth()
    moduleInstance.buildImpersonation()

    builder := melodysecurityconfig.NewBuilder()
    moduleInstance.RegisterSecurity(builder)

    return builder
}

/* The doors that write through the example into a backend it does not own — the object storage, the outbox, the message bus — and the platform check, which spends the distributed lock and three storage operations per call, are not public: each is asserted on its own so a rule moved back to public is named by the path that moved. */
func TestRegisterSecurity_TheDoorsThatWriteIntoABackendCarryARole(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    expectations := map[string]string{
        "/storage/object":      entity.RoleEditor,
        "/outbox/enqueue":      entity.RoleEditor,
        "/outbox/relay":        entity.RoleEditor,
        "/outbox/status":       entity.RoleEditor,
        "/messagebus/dispatch": entity.RoleEditor,
        "/platform/check":      entity.RoleUser,
    }

    for path, role := range expectations {
        attributes, matched := control.Match(path)

        if false == matched || 1 != len(attributes) || role != attributes[0] {
            t.Fatalf("expected %s to require %s, got matched=%v attributes=%v", path, role, matched, attributes)
        }
    }
}

/* The currency writes are catalogue data and carry the editor's requirement, while the listing keeps the user's; the listing's rule is a prefix of the writes, so each write path is asked on its own. */
func TestRegisterSecurity_TheCurrencyWritesRequireTheEditor(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    expectations := map[string]string{
        "/currencies/api/create/":         entity.RoleEditor,
        "/currencies/api/update/cur-eur/": entity.RoleEditor,
        "/currencies/api/delete/cur-eur/": entity.RoleEditor,
        "/currencies/api/read/":           entity.RoleUser,
    }

    for path, role := range expectations {
        attributes, matched := control.Match(path)

        if false == matched || 1 != len(attributes) || role != attributes[0] {
            t.Fatalf("expected %s to require %s, got matched=%v attributes=%v", path, role, matched, attributes)
        }
    }
}

/* The public rules are the readiness probe, the login and logout doors, the frontend bundle, the metrics and the openapi document, and the cipher round-trip probe, which reads nothing from the caller; the list is closed, so a rule added as public shows up here as the path that was not expected. */
func TestRegisterSecurity_ThePublicRulesAreTheClosedListTheReadmeStates(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    var public []string
    for _, rule := range control.Rules() {
        for _, attribute := range rule.Attributes() {
            if melodysecuritycontract.AttributePublicAccess == attribute {
                public = append(public, rule.Pattern()+rule.PathPrefix())
            }
        }
    }

    expected := []string{"/", "/index.html", "/login", "/logout", "/routes", "^/assets", "^/favicon", "^/(en|ro|ro-RO)/i18n(/|$)", "/health", "/metrics", "/openapi.json", "/encrypt/roundtrip"}

    if len(expected) != len(public) {
        t.Fatalf("expected %d public rules, got %d: %s", len(expected), len(public), strings.Join(public, ", "))
    }

    for index, description := range expected {
        if description != public[index] {
            t.Fatalf("expected public rule %d to be %q, got %q", index, description, public[index])
        }
    }
}

/* the greeting is public under the locales its route serves and nowhere else: an unlisted locale and the unprefixed spelling fall to the catch-all's reader requirement */
func TestRegisterSecurity_TheGreetingIsPublicOnlyUnderItsServedLocales(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    expectations := map[string]string{
        "/en/i18n/greeting/":    melodysecuritycontract.AttributePublicAccess,
        "/ro/i18n/greeting/":    melodysecuritycontract.AttributePublicAccess,
        "/ro-RO/i18n/greeting/": melodysecuritycontract.AttributePublicAccess,
        "/de/i18n/greeting/":    entity.RoleUser,
        "/i18n/greeting/":       entity.RoleUser,
        "/en/i18nx/greeting/":   entity.RoleUser,
    }

    for path, attribute := range expectations {
        attributes, matched := control.Match(path)

        if false == matched || 1 != len(attributes) || attribute != attributes[0] {
            t.Fatalf("expected %s to require %s, got matched=%v attributes=%v", path, attribute, matched, attributes)
        }
    }
}

/* a door that is one path is opened for that spelling alone: a neighbouring spelling such as /healthz falls to the catch-all's reader requirement, and the trailing slash stays one spelling */
func TestRegisterSecurity_TheOnePathDoorsArePublicForTheirOwnSpellingOnly(t *testing.T) {
    control := compiledSecurityModule(t).BuildAndCompile().GlobalAccessControl()

    expectations := map[string]string{
        "/health":              melodysecuritycontract.AttributePublicAccess,
        "/health/":             melodysecuritycontract.AttributePublicAccess,
        "/openapi.json":        melodysecuritycontract.AttributePublicAccess,
        "/login/":              melodysecuritycontract.AttributePublicAccess,
        "/healthz":             entity.RoleUser,
        "/health/extra":        entity.RoleUser,
        "/openapiXjson":        entity.RoleUser,
        "/login-admin":         entity.RoleUser,
        "/metrics2":            entity.RoleUser,
        "/encrypt/roundtrip/x": entity.RoleUser,
    }

    for path, attribute := range expectations {
        attributes, matched := control.Match(path)

        if false == matched || 1 != len(attributes) || attribute != attributes[0] {
            t.Fatalf("expected %s to require %s, got matched=%v attributes=%v", path, attribute, matched, attributes)
        }
    }
}

/* the machine firewall carries its whole policy: under overrideOnly it reads its one rule and nothing of the global list, which claims every path under /internal for the service role, and the global list names nothing under /internal, which it leaves to the catch-all */
func TestRegisterSecurity_TheInternalFirewallCarriesItsOwnRuleAlone(t *testing.T) {
    compiled := compiledSecurityModule(t).BuildAndCompile()

    var internalFirewall *melodysecurity.CompiledFirewall
    for _, firewall := range compiled.Firewalls() {
        if "internal" == firewall.Name() {
            internalFirewall = firewall
        }
    }

    if nil == internalFirewall {
        t.Fatal("expected the internal firewall to be compiled")
    }

    if 1 != len(internalFirewall.AccessControl().Rules()) {
        t.Fatalf("expected the internal firewall to read its one rule alone, got %d rules", len(internalFirewall.AccessControl().Rules()))
    }

    for _, path := range []string{"/internal", "/internal/whoami/", "/internal/not-routed/"} {
        attributes, matched := internalFirewall.AccessControl().Match(path)
        if false == matched || 1 != len(attributes) || internalCallerRole != attributes[0] {
            t.Fatalf("expected %s to require %s on the internal firewall, got matched=%v attributes=%v", path, internalCallerRole, matched, attributes)
        }
    }

    attributes, _ := compiled.GlobalAccessControl().Match("/internal/whoami/")
    if 1 != len(attributes) || entity.RoleUser != attributes[0] {
        t.Fatalf("expected the global list to leave /internal to the catch-all, got %v", attributes)
    }
}

/* with a metrics token the exposition is the scraper's alone: a firewall named metrics, registered ahead of main, authenticates exactly "Bearer <token>" as the scraper role, which is what /metrics requires; a wrong credential authenticates as nobody */
func TestRegisterSecurity_TheMetricsTokenPutsTheExpositionBehindTheScraperRole(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.metricsToken = "scrape-secret"
    moduleInstance.buildInternalAuth()
    moduleInstance.buildTokenAuth()
    moduleInstance.buildImpersonation()

    builder := melodysecurityconfig.NewBuilder()
    moduleInstance.RegisterSecurity(builder)
    compiled := builder.BuildAndCompile()

    attributes, _ := compiled.GlobalAccessControl().Match("/metrics")
    if 1 != len(attributes) || metricsScraperRole != attributes[0] {
        t.Fatalf("expected /metrics to require %s, got %v", metricsScraperRole, attributes)
    }

    var names []string
    var metricsFirewall *melodysecurity.CompiledFirewall
    for _, firewall := range compiled.Firewalls() {
        names = append(names, firewall.Name())
        if "metrics" == firewall.Name() {
            metricsFirewall = firewall
        }
    }

    if nil == metricsFirewall || "main" != names[len(names)-1] {
        t.Fatalf("expected a metrics firewall registered ahead of main, got %v", names)
    }

    for credential, authenticated := range map[string]bool{"Bearer scrape-secret": true, "Bearer wrong": false, "scrape-secret": false} {
        httpRequest := httptest.NewRequest(nethttp.MethodGet, "/metrics", nil)
        httpRequest.Header.Set("Authorization", credential)
        request := melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("metrics-test", time.Now()))

        containerInstance := melodycontainer.NewContainer()
        melodycontainer.MustRegister[melodyeventcontract.EventDispatcher](
            containerInstance,
            melodyevent.ServiceEventDispatcher,
            func(resolver melodycontainercontract.Resolver) (melodyeventcontract.EventDispatcher, error) {
                return melodyevent.NewEventDispatcher(melodyclock.NewSystemClock()), nil
            },
        )
        melodycontainer.MustRegister[melodyloggingcontract.Logger](
            containerInstance,
            melodylogging.ServiceLogger,
            func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
                return melodylogging.NewNopLogger(), nil
            },
        )
        runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

        token, _ := metricsFirewall.TokenSource().Resolve(runtimeInstance, request)
        if authenticated != (nil != token && true == token.IsAuthenticated()) {
            t.Fatalf("expected %q authenticated=%v, got %v", credential, authenticated, token)
        }
    }
}

/* the device firewall is wired over the account enricher: a stored device token resolves through the firewall's own source as authenticated, with the roles its account holds NOW, while the account exists, and as nobody once the account is gone */
func TestRegisterSecurity_TheDeviceFirewallHonoursATokenOnlyWhileItsAccountExists(t *testing.T) {
    for _, accountExists := range []bool{true, false} {
        moduleInstance := moduleWithEnvironment(t, map[string]string{})
        moduleInstance.buildInternalAuth()
        moduleInstance.buildTokenAuth()
        moduleInstance.buildImpersonation()

        moduleInstance.opaqueTokenStore.Put("device-token", melodysecuritycontract.Claims{UserIdentifier: "user-2", DeviceIdentifier: "phone", Roles: []string{entity.RoleAdmin}})

        builder := melodysecurityconfig.NewBuilder()
        moduleInstance.RegisterSecurity(builder)

        var deviceFirewall *melodysecurity.CompiledFirewall
        for _, firewall := range builder.BuildAndCompile().Firewalls() {
            if "deviceToken" == firewall.Name() {
                deviceFirewall = firewall
            }
        }

        if nil == deviceFirewall {
            t.Fatal("expected the device firewall to be compiled")
        }

        storage := persistence.NewCatalogStorage(nil)
        if true == accountExists {
            storage = storage.WithAccountSeed()
        }

        userRepository, repositoryErr := repository.NewUserRepository(storage)
        if nil != repositoryErr {
            t.Fatalf("new user repository: %v", repositoryErr)
        }

        containerInstance := melodycontainer.NewContainer()
        opaqueTokenStore := moduleInstance.opaqueTokenStore
        melodycontainer.MustRegister[melodysecuritycontract.RevocableTokenStore](containerInstance, melodyrueidis.ServiceTokenStore, func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.RevocableTokenStore, error) {
            return opaqueTokenStore, nil
        })
        melodycontainer.MustRegister[repository.UserRepository](containerInstance, repository.ServiceUserRepository, func(resolver melodycontainercontract.Resolver) (repository.UserRepository, error) {
            return userRepository, nil
        })
        melodycontainer.MustRegister[melodyeventcontract.EventDispatcher](containerInstance, melodyevent.ServiceEventDispatcher, func(resolver melodycontainercontract.Resolver) (melodyeventcontract.EventDispatcher, error) {
            return melodyevent.NewEventDispatcher(melodyclock.NewSystemClock()), nil
        })
        melodycontainer.MustRegister[melodyloggingcontract.Logger](containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        })
        runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

        httpRequest := httptest.NewRequest(nethttp.MethodGet, route.DeviceIdentityPattern, nil)
        httpRequest.Header.Set("Authorization", "Bearer device-token")
        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("device-test", time.Now()))

        token, _ := deviceFirewall.TokenSource().Resolve(runtimeInstance, request)
        authenticated := nil != token && true == token.IsAuthenticated()

        if false == accountExists {
            if true == authenticated {
                t.Fatalf("expected the token of an absent account resolved as nobody, got %v", token.Roles())
            }

            continue
        }

        if false == authenticated || 1 != len(token.Roles()) || entity.RoleEditor != token.Roles()[0] {
            t.Fatalf("expected the token authenticated with the account's current roles [%s], got %v", entity.RoleEditor, token)
        }
    }
}
