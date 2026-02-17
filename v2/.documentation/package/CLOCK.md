# CLOCK

The [`clock`](../../clock) package provides an abstraction over time for deterministic behavior in tests and for framework components that need to read the current time or create tickers.

## Scope

Melody uses a clock internally (for example, the event dispatcher is clock-driven), and the kernel exposes a clock instance via its API.

This package also provides optional container integration helpers for userland services that want to resolve a `clock/contract.Clock` from the service container.

## Subpackages

- [`clock/contract`](../../clock/contract)  
  Public contracts (`Clock`, `Ticker`) implemented by the provided clock implementations.

## Responsibilities

- Provide production and test clock implementations:
    - [`SystemClock`](../../clock/system_clock.go)
    - [`FrozenClock`](../../clock/frozen_clock.go)
- Provide typed contracts for consuming code:
    - [`clockcontract.Clock`](../../clock/contract/clock.go)
    - [`clockcontract.Ticker`](../../clock/contract/ticker.go)
- Provide container resolver helpers:
    - [`ClockMustFromContainer`](../../clock/service_resolver.go)
    - [`ClockMustFromResolver`](../../clock/service_resolver.go)

## Container integration

The package defines the service name:

- [`ServiceClock`](../../clock/service_resolver.go) (`"service.clock"`)

If you want your own services to resolve a clock from the container, register `ServiceClock` as a `clockcontract.Clock` provider.

```go
package main

import (
	"time"

	"github.com/precision-soft/melody/v2/clock"
	clockcontract "github.com/precision-soft/melody/v2/clock/contract"
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
)

func registerFrozenClock(
	serviceContainer containercontract.Container,
) {
	frozenClock := clock.NewFrozenClock(
		time.Date(
			2026,
			1,
			16,
			10,
			0,
			0,
			0,
			time.UTC,
		),
	)

	serviceContainer.MustRegister(
		clock.ServiceClock,
		func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
			return frozenClock, nil
		},
	)
}

func readCurrentTime(
	serviceContainer containercontract.Container,
) time.Time {
	clockInstance := clock.ClockMustFromContainer(serviceContainer)

	return clockInstance.Now()
}

func example() time.Time {
	serviceContainer := container.NewContainer()

	registerFrozenClock(serviceContainer)

	return readCurrentTime(serviceContainer)
}
```

## Footguns & caveats

- Registering `ServiceClock` only affects code that **resolves the clock from the container**.  
  The kernel stores its own clock instance (passed at construction time) and does not auto-resolve the clock from the container.
- `ClockMustFromContainer` / `ClockMustFromResolver` are fail-fast helpers. They will panic (via Melody’s container/service resolver semantics) when the clock service is missing or has an invalid type.

## Userland API

### Contracts (`clock/contract`)

- [`type Clock`](../../clock/contract/clock.go)  
  `Now() time.Time`  
  `NewTicker(interval time.Duration) clockcontract.Ticker`
- [`type Ticker`](../../clock/contract/ticker.go)  
  `Channel() <-chan time.Time`  
  `Stop()`

### Implementations (`clock`)

- [`type SystemClock`](../../clock/system_clock.go)
    - [`NewSystemClock()`](../../clock/system_clock.go)
    - `Now() time.Time`
    - `NewTicker(interval time.Duration) clockcontract.Ticker`
- [`type FrozenClock`](../../clock/frozen_clock.go)
    - [`NewFrozenClock(time.Time)`](../../clock/frozen_clock.go)
    - `Now() time.Time`
    - `TravelTo(time.Time)`
    - `Advance(time.Duration)`
    - `NewTicker(interval time.Duration) clockcontract.Ticker`

### Container helpers (`clock`)

- [`const ServiceClock`](../../clock/service_resolver.go)
- [`ClockMustFromContainer(containercontract.Container) clockcontract.Clock`](../../clock/service_resolver.go)
- [`ClockMustFromResolver(containercontract.Resolver) clockcontract.Clock`](../../clock/service_resolver.go)
