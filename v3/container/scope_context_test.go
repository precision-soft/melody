package container

import (
    "context"
    "errors"
    "reflect"
    "strings"
    "time"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type scopeContextProbe struct {
    plainCalls int
    contexts   []context.Context
    closeErr   error
    panicValue any
}

func (instance *scopeContextProbe) Close() error {
    instance.plainCalls++
    return nil
}

func (instance *scopeContextProbe) CloseWithContext(closeContext context.Context) error {
    instance.contexts = append(instance.contexts, closeContext)
    if nil != instance.panicValue {
        panic(instance.panicValue)
    }
    return instance.closeErr
}

func TestScopeClose_PrefersContextCloser(t *testing.T) {
    serviceContainer := NewContainer()
    probe := &scopeContextProbe{}
    if err := serviceContainer.RegisterScoped("probe", func(containercontract.Resolver) (*scopeContextProbe, error) {
        return probe, nil
    }); nil != err {
        t.Fatal(err)
    }
    requestScope := serviceContainer.NewScope()
    requestScope.MustGet("probe")
    if err := requestScope.Close(); nil != err {
        t.Fatal(err)
    }
    if 0 != probe.plainCalls || 1 != len(probe.contexts) {
        t.Fatalf("expected context close once and no plain close, got context=%d plain=%d", len(probe.contexts), probe.plainCalls)
    }
}

func TestScopeCloseWithContext_PreservesCallerContextAndEvictedInstances(t *testing.T) {
    for _, expired := range []bool{false, true} {
        t.Run(map[bool]string{false: "live", true: "expired"}[expired], func(t *testing.T) {
            closeContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
            defer cancel()
            if expired {
                cancel()
            }
            serviceContainer := NewContainer()
            original := &scopeContextProbe{}
            replacement := &scopeContextProbe{}
            if err := serviceContainer.RegisterScoped("probe", func(containercontract.Resolver) (*scopeContextProbe, error) {
                return original, nil
            }); nil != err {
                t.Fatal(err)
            }
            requestScope := serviceContainer.NewScope()
            requestScope.MustGetByType(reflect.TypeOf(original))
            overridingScope := requestScope.(containercontract.OverrideServiceWithOptions)
            if err := overridingScope.OverrideInstanceWithOptions("probe", replacement, ClosedWithScope()); nil != err {
                t.Fatal(err)
            }
            contextCloser, supported := requestScope.(interface { CloseWithContext(context.Context) error })
            if false == supported {
                t.Fatal("scope lacks optional context closer")
            }
            if err := contextCloser.CloseWithContext(closeContext); nil != err {
                t.Fatal(err)
            }
            if err := requestScope.Close(); nil != err {
                t.Fatal(err)
            }
            for _, probe := range []*scopeContextProbe{original, replacement} {
                if 0 != probe.plainCalls || 1 != len(probe.contexts) {
                    t.Fatalf("expected exactly one context close, got context=%d plain=%d", len(probe.contexts), probe.plainCalls)
                }
                if closeContext != probe.contexts[0] {
                    t.Fatal("service did not receive the caller's original context")
                }
                if expired != (nil != probe.contexts[0].Err()) {
                    t.Fatal("service did not observe the caller's cancellation")
                }
            }
        })
    }
}

type scopeLegacyContextControl struct {
    calls int
}

func (instance *scopeLegacyContextControl) Close() error {
    instance.calls++
    return nil
}

func TestScopeCloseWithContext_ContinuesAfterFailure(t *testing.T) {
    for _, panicking := range []bool{false, true} {
        t.Run(map[bool]string{false: "error", true: "panic"}[panicking], func(t *testing.T) {
            serviceContainer := NewContainer()
            legacy := &scopeLegacyContextControl{}
            probe := &scopeContextProbe{}
            failure := errors.New("scope context failure")
            if panicking {
                probe.panicValue = failure
            } else {
                probe.closeErr = failure
            }
            if err := serviceContainer.RegisterScoped("legacy", func(containercontract.Resolver) (*scopeLegacyContextControl, error) {
                return legacy, nil
            }); nil != err {
                t.Fatal(err)
            }
            if err := serviceContainer.RegisterScoped("probe", func(resolver containercontract.Resolver) (*scopeContextProbe, error) {
                resolver.MustGet("legacy")
                return probe, nil
            }); nil != err {
                t.Fatal(err)
            }
            requestScope := serviceContainer.NewScope()
            requestScope.MustGet("probe")
            closeContext, cancel := context.WithCancel(context.Background())
            cancel()
            err := requestScope.(interface { CloseWithContext(context.Context) error }).CloseWithContext(closeContext)
            if nil == err || false == strings.Contains(err.Error(), "failed to close scope services") {
                t.Fatalf("expected aggregate scope error, got %v", err)
            }
            if 1 != legacy.calls || 1 != len(probe.contexts) || 0 != probe.plainCalls {
                t.Fatalf("teardown stopped or used plain fallback: legacy=%d context=%d plain=%d", legacy.calls, len(probe.contexts), probe.plainCalls)
            }
        })
    }
}
