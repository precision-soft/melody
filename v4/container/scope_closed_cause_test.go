package container

import (
    "context"
    "errors"
    "io"
    "reflect"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type closedCauseCloser struct{}

func (instance *closedCauseCloser) Close() error { return nil }

func (instance *closedCauseCloser) CloseWithContext(closeContext context.Context) error { return nil }

func closedScopeRefusals(t *testing.T, boot bool) map[string]error {
    t.Helper()

    serviceContainer := NewContainer(ContainerConfig{}).(*container)
    MustRegister[*closedCauseCloser](serviceContainer, "app.closed.cause.closer", func(resolver containercontract.Resolver) (*closedCauseCloser, error) {
        return &closedCauseCloser{}, nil
    }, RegisterOptions{})

    var closedScope containercontract.Scope
    if true == boot {
        closedScope = serviceContainer.NewBootScope()
    } else {
        closedScope = serviceContainer.NewScope()
    }

    retainedView := newScopeResolverContext(serviceContainer, closedScope.(*scope))
    handle := Lazy[*closedCauseCloser](closedScope, "app.closed.cause.closer")

    if closeErr := closedScope.Close(); nil != closeErr {
        t.Fatalf("close the scope: %v", closeErr)
    }

    refusals := map[string]error{}
    _, refusals["Get"] = closedScope.Get(t.Context(), "app.closed.cause.closer")
    _, refusals["GetByType"] = closedScope.GetByType(t.Context(), reflect.TypeOf((*closedCauseCloser)(nil)))
    refusals["OverrideProtectedInstance"] = closedScope.OverrideProtectedInstance("app.closed.cause.closer", &closedCauseCloser{}, containercontract.OverrideOptions{})
    refusals["RegisterScoped"] = closedScope.RegisterScoped("app.closed.cause.scoped", func(resolver containercontract.Resolver) (string, error) { return "", nil }, containercontract.RegisterOptions{})
    _, refusals["AllImplementing"] = AllImplementing[io.Closer](t.Context(), closedScope)
    _, refusals["Lazy"] = handle.Resolve(serviceContainer.Runtime())
    _, refusals["retained view"] = retainedView.Get(t.Context(), "app.closed.cause.closer")

    return refusals
}

func TestScopeClosedCause_EveryRefusalOfAClosedBootScopeNamesTheFinishedBoot(t *testing.T) {
    for door, refusal := range closedScopeRefusals(t, true) {
        /* the boot scope refuses a scoped registration before it asks whether it is closed */
        if "RegisterScoped" == door {
            if false == errors.Is(refusal, ErrScopedServiceAtBoot) {
                t.Fatalf("%s: expected ErrScopedServiceAtBoot, got %v", door, refusal)
            }

            continue
        }

        if false == errors.Is(refusal, ErrScopeClosed) || false == errors.Is(refusal, containercontract.ErrBootFinished) {
            t.Fatalf("%s: expected both ErrScopeClosed and ErrBootFinished, got %v", door, refusal)
        }
    }
}

func TestScopeClosedCause_AClosedRequestScopeDoesNotNameTheBoot(t *testing.T) {
    for door, refusal := range closedScopeRefusals(t, false) {
        if false == errors.Is(refusal, ErrScopeClosed) || true == errors.Is(refusal, containercontract.ErrBootFinished) {
            t.Fatalf("%s: expected ErrScopeClosed alone, got %v", door, refusal)
        }
    }
}

func TestScopeClosedCause_TheBootCauseNamesTheBootScope(t *testing.T) {
    if "the boot scope is closed: the application boot has finished" != errBootScopeClosed.Error() {
        t.Fatalf("the boot cause reads %q", errBootScopeClosed.Error())
    }

    if nil != errors.Unwrap(errBootScopeClosed) {
        t.Fatalf("the boot cause unwraps, so the chain repeats the sentinels")
    }
}
