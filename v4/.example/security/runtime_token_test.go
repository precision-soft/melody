package security

import (
    "context"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

func runtimeWithoutSecurityContext(t *testing.T) melodycontainercontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() {
        _ = containerInstance.CloseWithContext(context.Background())
    })

    return melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
}

func runtimeCarryingToken(t *testing.T, token melodysecuritycontract.Token) melodycontainercontract.Runtime {
    t.Helper()

    runtimeInstance := runtimeWithoutSecurityContext(t)

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, token),
    )

    return runtimeInstance
}

func TestTokenFromRuntime_AnswersTheLiveToken(t *testing.T) {
    published := melodysecurity.NewAuthenticatedToken("ada", []string{"ROLE_ADMIN"}, nil)

    token, exists := TokenFromRuntime(runtimeCarryingToken(t, published))

    if false == exists {
        t.Fatalf("expected the published token to be answered as present")
    }

    if "ada" != token.UserIdentifier() {
        t.Fatalf("answered %q, wanted the published token's identifier", token.UserIdentifier())
    }
}

func TestTokenFromRuntime_AnswersAbsentWithoutASecurityContext(t *testing.T) {
    token, exists := TokenFromRuntime(runtimeWithoutSecurityContext(t))

    if true == exists {
        t.Fatalf("expected absence, answered %v", token)
    }
}

/* the security context is built here with no token at all, which the framework's
resolution listener never does -- each of its exits publishes a live token. The
producer that can is the exported constructor, used exactly as this test uses it. */
func TestTokenFromRuntime_AnswersAbsentForAContextCarryingNoToken(t *testing.T) {
    token, exists := TokenFromRuntime(runtimeCarryingToken(t, nil))

    if true == exists {
        t.Fatalf("expected a context with no token to read as absent, answered %v", token)
    }
}
