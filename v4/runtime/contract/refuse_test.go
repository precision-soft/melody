package contract

import (
    "context"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type contextOnlyRuntime struct {
    ctx context.Context
}

func (instance *contextOnlyRuntime) Context() context.Context {
    return instance.ctx
}

func (instance *contextOnlyRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance *contextOnlyRuntime) Container() containercontract.Container {
    return nil
}

func TestRefuseNilRuntime_RefusesNilATypedNilAndANilContextNamingTheArgument(t *testing.T) {
    var typedNil *contextOnlyRuntime

    for name, runtimeInstance := range map[string]Runtime{"nil": nil, "typed nil": typedNil, "nil context": &contextOnlyRuntime{}} {
        refusal := RefuseNilRuntime(runtimeInstance)
        if "runtimeInstance" != exception.LogContext(refusal, exception.LogContextConfig{})["argument"] || true == exception.IsInfrastructureFailure(refusal) {
            t.Fatalf("expected the %s runtime refused unmarked naming the argument, got %v", name, refusal)
        }
    }
}

func TestRefuseNilRuntime_AdmitsARuntimeWithAContext(t *testing.T) {
    if refusal := RefuseNilRuntime(&contextOnlyRuntime{ctx: context.Background()}); nil != refusal {
        t.Fatalf("expected a runtime with a context admitted, got %v", refusal)
    }
}
