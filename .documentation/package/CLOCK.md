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

`ServiceClock` is **already registered by the application during boot** — [`(*Application).bootContainer`](../../application/application_container.go) binds it to the kernel's own clock unconditionally, so `ClockMustFromResolver` works out of the box in any service, command or handler. Do **not** register it again from userland: a duplicate service id is recorded as a boot collision and the boot panics.

Registering `ServiceClock` yourself is therefore only for a container you build and own — a unit test that assembles a bare `container.NewContainer()` rather than an `Application`, as below:

```go
package main

import (
	"time"

	"github.com/precision-soft/melody/clock"
	clockcontract "github.com/precision-soft/melody/clock/contract"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
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

- On an `Application`, `ServiceClock` is registered during boot; a userland registration of the same id is a **boot collision** and the boot panics. Register it only in a container you assembled yourself.
- `NewApplication` builds the kernel with a [`SystemClock`](../../clock/system_clock.go) and there is no hook to substitute another one, so a `FrozenClock` cannot be injected into a booted application. Freeze time by constructing your own container/kernel in the test, or by taking a `clockcontract.Clock` as a constructor parameter of the service under test.
- Registering `ServiceClock` only affects code that **resolves the clock from the container**.  
  The kernel stores its own clock instance (passed at construction time) and does not auto-resolve the clock from the container.
- `ClockMustFromContainer` / `ClockMustFromResolver` are fail-fast helpers. They panic on a nil (or typed-nil) container/resolver, naming the argument, and panic (via Melody's container/service resolver semantics) when the clock service is missing or has an invalid type.
- **The frozen clock's ticker runs on real wall time.** The tick cadence and the frozen timeline are two different timelines: `Advance`/`TravelTo` never fire a tick and never suppress one, and each tick that does fire carries the frozen clock's current `Now()` at delivery time — a tick racing a `TravelTo` may carry either the pre-travel or the post-travel instant. A test that advances the frozen clock past a ticker interval and then waits for a tick does not wait forever — it waits the **real** interval, because the underlying ticker is a real `time.Ticker` that the advance neither fired nor brought closer. A test that needs a tick must wait out that real interval; one that cannot afford to should not be driving a ticker.
- **`Ticker.Stop` is mandatory, on every implementation.** The frozen ticker owns a relay goroutine, and abandoning it without `Stop` leaks that goroutine and pins the ticker and its clock for the life of the process. `Stop` is idempotent, never closes the channel — a consumer selecting on a stopped ticker's channel would spin on the zero value from a closed one — and returns only after the relay goroutine has exited, so no tick can be minted after `Stop` returns; a tick accepted into the buffered channel **before** `Stop` may still be read afterwards, exactly as with `time.Ticker`.
- **`Advance` is forward-only** and panics on a negative duration: moving the frozen clock backwards silently broke the monotonic invariants the code under test relies on. `TravelTo` remains the deliberate door for backwards motion.
- `NewTicker` panics on a non-positive interval, on both implementations, with the interval in the error context.

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
