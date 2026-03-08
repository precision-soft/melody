# DEBUG

The [`debug`](../../debug) package provides built-in CLI debug commands for inspecting Melody runtime wiring.

## Scope

This package exposes `clicontract.Command` implementations that can be registered into a Melody CLI command context.

The commands are intended for diagnostics and local debugging (container services, events, HTTP router, middleware order, parameters, and version metadata).

## Responsibilities

- Provide ready-to-register debug commands:
    - container services (`debug:container`)
    - event listeners (`debug:events`)
    - HTTP router routes (`debug:router`)
    - HTTP middleware order (`debug:middleware`)
    - parameters (`debug:parameters`)
    - version metadata (`debug:version`)

## Exported API

### Commands

- [`ContainerCommand`](../../debug/command_container.go)
- [`EventCommand`](../../debug/command_event.go)
- [`RouterCommand`](../../debug/command_router.go)
- [`ParameterCommand`](../../debug/command_parameter.go)
- [`VersionCommand`](../../debug/command_version.go)
- [`MiddlewareCommand`](../../debug/command_middleware.go)

### Constructors and helpers

- [`NewMiddlewareCommand(middlewareProvider MiddlewareProvider) *MiddlewareCommand`](../../debug/command_middleware.go)
- [`MiddlewareProvider`](../../debug/command_middleware.go)

## Usage

```go
package main

import (
	"context"

	"github.com/precision-soft/melody/v2/cli"
	clicontract "github.com/precision-soft/melody/v2/cli/contract"
	"github.com/precision-soft/melody/v2/container"
	"github.com/precision-soft/melody/v2/debug"
	"github.com/precision-soft/melody/v2/exception"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	"github.com/precision-soft/melody/v2/runtime"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func main() {
	ctx := context.Background()

	serviceContainer := container.NewContainer()
	scope := serviceContainer.NewScope()

	runtimeInstance := runtime.New(
		ctx,
		scope,
		runtime.WithDefaultLogger(),
	)

	commandContext := cli.NewCommandContext(
		"example",
		"example application",
	)

	registerErr := cli.Register(
		commandContext,
		&debug.ContainerCommand{},
		runtimeInstance,
	)
	if nil != registerErr {
		exception.Panic(registerErr)
	}

	registerErr = cli.Register(
		commandContext,
		&debug.EventCommand{},
		runtimeInstance,
	)
	if nil != registerErr {
		exception.Panic(registerErr)
	}

	registerErr = cli.Register(
		commandContext,
		&debug.ParameterCommand{},
		runtimeInstance,
	)
	if nil != registerErr {
		exception.Panic(registerErr)
	}

	registerErr = cli.Register(
		commandContext,
		&debug.RouterCommand{},
		runtimeInstance,
	)
	if nil != registerErr {
		exception.Panic(registerErr)
	}

	registerErr = cli.Register(
		commandContext,
		debug.NewMiddlewareCommand(
			func() []httpcontract.Middleware {
				return []httpcontract.Middleware{}
			},
		),
		runtimeInstance,
	)
	if nil != registerErr {
		exception.Panic(registerErr)
	}

	registerErr = cli.Register(
		commandContext,
		&debug.VersionCommand{ApplicationVersion: "v1.0.0"},
		runtimeInstance,
	)
	if nil != registerErr {
		exception.Panic(registerErr)
	}

	runErr := commandContext.Run(
		ctx,
		[]string{"example", "debug:version"},
	)
	if nil != runErr {
		exception.Panic(runErr)
	}

	shutdownErr := shutdownRuntime(runtimeInstance)
	if nil != shutdownErr {
		exception.Panic(shutdownErr)
	}
}

func shutdownRuntime(runtimeInstance runtimecontract.Runtime) error {
	return runtimeInstance.Shutdown(
		context.Background(),
	)
}

var _ clicontract.Command = (*debug.ContainerCommand)(nil)
```
