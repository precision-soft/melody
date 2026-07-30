package security

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/runtime"
)

/* erroringResolverScope answers Has affirmatively but fails every Get, modelling the window in which the security context is present when Has runs and the scope is closed by the time Get runs. */
type erroringResolverScope struct {
    *testScope
}

func (instance *erroringResolverScope) Has(serviceName string) bool {
    return true
}

func (instance *erroringResolverScope) Get(serviceName string) (any, error) {
    return nil, errors.New("scope is closed")
}

/* @info SecurityContextFromRuntime runs from IsGranted, which a handler can call from a goroutine that outlives the request; resolving the logger with the panicking Must variant on a closed scope crashes that uncovered goroutine, so a failed resolution must return (nil, false) rather than panic */
func TestSecurityContextFromRuntime_DoesNotPanicWhenResolutionFails(t *testing.T) {
    scope := &erroringResolverScope{testScope: newTestScope()}
    runtimeInstance := runtime.New(context.Background(), scope, container.NewContainer())

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if true == exists {
        t.Fatalf("expected exists to be false when resolution fails")
    }
    if nil != securityContext {
        t.Fatalf("expected a nil security context when resolution fails")
    }
}
