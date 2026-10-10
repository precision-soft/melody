package security

import (
    "context"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func runtimeWithoutSecurityContext(t *testing.T) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() {
        _ = containerInstance.Close()
    })

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func runtimeCarryingToken(t *testing.T, token melodysecuritycontract.Token) melodyruntimecontract.Runtime {
    t.Helper()

    runtimeInstance := runtimeWithoutSecurityContext(t)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil, nil, nil, nil, nil, nil, nil,
        "", "",
        nil, nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, token),
    )

    return runtimeInstance
}

func TestTokenFromRuntime_AnswersTheLiveToken(t *testing.T) {
    published := melodysecurity.NewAuthenticatedToken("ada", []string{"ROLE_ADMIN"})

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
