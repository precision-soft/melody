package service

import (
    "strings"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyhttp "github.com/precision-soft/melody/http"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

/* requestScope builds the shape the http kernel produces: a scope carrying the request context, and optionally the security context the firewall published. */
func requestScope(t *testing.T, token melodysecuritycontract.Token, withSecurityContext bool) melodycontainercontract.Scope {
    t.Helper()

    scope := melodycontainer.NewContainer().NewScope()
    scope.MustOverrideProtectedInstance(
        melodyhttp.ServiceRequestContext,
        melodyhttp.NewRequestContext("request-1", time.Now()),
    )

    if true == withSecurityContext {
        firewall := melodysecurity.NewCompiledFirewall(
            "main",
            melodysecurity.NewPathPrefixMatcher("/"),
            "/",
            nil,
            nil,
            nil,
            nil,
            nil,
            nil,
            nil,
            "",
            "",
            nil,
            nil,
            melodysecurity.SourceGlobal,
            melodysecurity.SourceGlobal,
            melodysecurity.SourceGlobal,
            melodysecurity.SourceGlobal,
            melodysecurity.SourceGlobal,
        )
        scope.MustOverrideProtectedInstance(
            melodysecuritycontract.ServiceSecurityContext,
            melodysecurity.NewSecurityContext(firewall, token),
        )
    }

    return scope
}

/* the scoped door refuses the framework's protected "service." namespace outright, so the registration is proven through the REAL door: a name drifting back into that namespace dies here instead of at the first boot */
func TestChangeAttributionRegistersThroughTheScopedDoor(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegisterScoped[*ChangeAttribution](
        serviceContainer,
        ServiceChangeAttribution,
        ChangeAttributionFromResolver,
    )
}

func TestChangeAttributionRefusesAScopeWithoutARequestContext(t *testing.T) {
    scope := melodycontainer.NewContainer().NewScope()

    attribution, attributionErr := ChangeAttributionFromResolver(scope)
    if nil == attributionErr {
        t.Fatal("expected a scope without a request context to be refused")
    }
    if nil != attribution {
        t.Fatalf("expected no attribution beside the refusal, got %v", attribution)
    }
    if false == strings.Contains(attributionErr.Error(), "request scope") {
        t.Fatalf("expected the refusal to say why, got %v", attributionErr)
    }
}

func TestChangeAttributionNamesTheSignedInUserOfTheRequestScope(t *testing.T) {
    scope := requestScope(t, melodysecurity.NewAuthenticatedToken("  user-5  ", []string{"ROLE_EDITOR"}), true)

    attribution, attributionErr := ChangeAttributionFromResolver(scope)
    if nil != attributionErr {
        t.Fatalf("expected the request scope to be accepted, got %v", attributionErr)
    }
    if "user-5" != attribution.Actor() {
        t.Fatalf("unexpected actor: %q", attribution.Actor())
    }
}

func TestChangeAttributionAnswersTheSystemOnTheRuntimeFallbackVerdicts(t *testing.T) {
    for name, build := range map[string]func(*testing.T) melodycontainercontract.Scope{
        "no security context on the scope": func(t *testing.T) melodycontainercontract.Scope {
            return requestScope(t, nil, false)
        },
        "a security context holding no token": func(t *testing.T) melodycontainercontract.Scope {
            return requestScope(t, nil, true)
        },
        "an anonymous token": func(t *testing.T) melodycontainercontract.Scope {
            return requestScope(t, melodysecurity.NewAnonymousToken(), true)
        },
        "an authenticated token with a blank identifier": func(t *testing.T) melodycontainercontract.Scope {
            return requestScope(t, melodysecurity.NewAuthenticatedToken("   ", []string{"ROLE_USER"}), true)
        },
    } {
        t.Run(name, func(t *testing.T) {
            attribution, attributionErr := ChangeAttributionFromResolver(build(t))
            if nil != attributionErr {
                t.Fatalf("expected the request scope to be accepted, got %v", attributionErr)
            }
            if "system" != attribution.Actor() {
                t.Fatalf("unexpected actor: %q", attribution.Actor())
            }
        })
    }
}
