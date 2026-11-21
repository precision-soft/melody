package contract

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

type contextOnlyRuntime struct {
    ctx context.Context
}

func (instance *contextOnlyRuntime) Context() context.Context {
    return instance.ctx
}

func (instance *contextOnlyRuntime) Scope() Scope {
    return nil
}

func (instance *contextOnlyRuntime) Kind() Kind {
    return KindRun
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

type kindOnlyRuntime struct {
    contextOnlyRuntime
    kind Kind
}

func (instance *kindOnlyRuntime) Kind() Kind {
    return instance.kind
}

func TestRefuseRuntimeKind_AdmitsARequiredKindAndRefusesAnotherNamingTheLookupAndBothKinds(t *testing.T) {
    requestRuntime := &kindOnlyRuntime{contextOnlyRuntime: contextOnlyRuntime{ctx: context.Background()}, kind: KindRequest}

    if refusal := RefuseRuntimeKind(requestRuntime, "Lookup", []Kind{KindRun, KindRequest}); nil != refusal {
        t.Fatalf("expected a required kind to be admitted, got %v", refusal)
    }

    refusal := RefuseRuntimeKind(requestRuntime, "Lookup", []Kind{KindBoot, KindRun})
    if false == errors.Is(refusal, ErrRuntimeKindRefused) {
        t.Fatalf("expected a refusal under ErrRuntimeKindRefused, got %v", refusal)
    }

    var melodyErr *exception.Error
    if false == errors.As(refusal, &melodyErr) || "Lookup" != melodyErr.Context()["lookup"] || "boot, run" != melodyErr.Context()["required"] || "request" != melodyErr.Context()["given"] {
        t.Fatalf("expected the refusal to name the lookup and both kinds, got %v", refusal)
    }

    if nil == RefuseRuntimeKind(nil, "Lookup", []Kind{KindRequest}) {
        t.Fatalf("expected a nil runtime to be refused")
    }
}
