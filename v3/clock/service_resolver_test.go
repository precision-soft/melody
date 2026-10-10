package clock

import (
    "testing"

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestClockServiceResolvers(t *testing.T) {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        ServiceClock,
        func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
            return NewSystemClock(), nil
        },
    )

    clockFromContainer := ClockMustFromContainer(serviceContainer)
    if nil == clockFromContainer {
        t.Fatalf("expected clock from container")
    }

    clockFromResolver := ClockMustFromResolver(serviceContainer)
    if nil == clockFromResolver {
        t.Fatalf("expected clock from resolver")
    }

    now := clockFromResolver.Now()
    if true == now.IsZero() {
        t.Fatalf("expected Now() to return non-zero time")
    }
}

func TestClockMustFromContainer_RefusesNilContainer(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromContainer(nil)
        },
        "container may not be nil",
    )

    var typedNil *serviceResolverTestContainer
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromContainer(typedNil)
        },
        "container may not be nil",
    )
}

/* a nil pointer whose promoted interface methods are never reached: the guard under test must fire before any of them would dereference the nil receiver */
type serviceResolverTestContainer struct {
    containercontract.Container
}

func TestClockMustFromResolver_RefusesNilResolver(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromResolver(nil)
        },
        "resolver may not be nil",
    )
}

/* a nil pointer whose promoted interface methods are never reached: the resolver door's guard must fire before any of them would dereference the nil receiver */
type serviceResolverTestResolver struct {
    containercontract.Resolver
}

/* the container door already refuses a typed nil; the resolver door is its twin and was pinned on no major — only the untyped nil was, which a plain comparison answers just as well. */
func TestClockMustFromResolver_RefusesATypedNilResolver(t *testing.T) {
    var typedNil *serviceResolverTestResolver

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromResolver(typedNil)
        },
        "resolver may not be nil",
    )
}
