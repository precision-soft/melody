package clock

import (
	"testing"

	clockcontract "github.com/precision-soft/melody/clock/contract"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
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
