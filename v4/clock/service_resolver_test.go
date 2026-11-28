package clock

import (
    "testing"

    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestClockServiceResolvers(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        ServiceClock,
        func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
            return NewSystemClock(), nil
        },
        container.RegisterOptions{},
    )

    clockFromContainer := ClockMustFromResolver(t.Context(), serviceContainer)
    if nil == clockFromContainer {
        t.Fatalf("expected clock from container")
    }

    clockFromResolver := ClockMustFromResolver(t.Context(), serviceContainer)
    if nil == clockFromResolver {
        t.Fatalf("expected clock from resolver")
    }

    now := clockFromResolver.Now()
    if true == now.IsZero() {
        t.Fatalf("expected Now() to return non-zero time")
    }
}

func TestClockMustFromResolver_RefusesANilResolver(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromResolver(t.Context(), nil)
        },
        "resolver may not be nil",
    )

    var typedNil *serviceResolverTestContainer
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromResolver(t.Context(), typedNil)
        },
        "resolver may not be nil",
    )
}

type serviceResolverTestContainer struct {
    containercontract.Container
}

func TestClockMustFromResolver_RefusesNilResolver(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromResolver(t.Context(), nil)
        },
        "resolver may not be nil",
    )
}

type serviceResolverTestResolver struct {
    containercontract.Resolver
}

func TestClockMustFromResolver_RefusesATypedNilResolver(t *testing.T) {
    var typedNil *serviceResolverTestResolver

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = ClockMustFromResolver(t.Context(), typedNil)
        },
        "resolver may not be nil",
    )
}
