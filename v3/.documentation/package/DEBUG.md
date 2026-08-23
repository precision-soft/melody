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

`debug:parameters` reports a `secret` column: a parameter declared as holding a credential (see [CONFIG](CONFIG.md#secret-parameters)) renders its value as `********` — or `(empty)` when it carries none — with the length withheld, while an ordinary parameter prints in the clear.

`debug:events --format=json --verbose` carries the per-listener detail — priority, source, owner and the required / may-skip marks — under `data.listeners`, beside the event list under `data.events`; without `--verbose` the json `data` stays the plain list payload. The **declaration** of the listeners a serving process wires and this one does not is on `data.servingProcessListeners` at every verbosity, beside the listing rather than instead of it, because it exists so that "is access control wired?" is not answered with an absence meaning "not in this process". `--order` reaches both halves of the document: it orders the events, while inside one event the listener rows keep the dispatch order their `order` field reports. The table summary's `SUBSCRIBERS` total counts distinct subscribers across the whole dispatcher, while the per-event column counts them per event.

## Flags and output

Every debug command declares [`output.DebugFlags()`](../../cli/output/standard_flag.go) as its flag set and renders its result through [`output.Render`](../../cli/output/renderer.go), so all six share one interface. `DebugFlags()` is [`output.StandardFlags()`](../../cli/output/standard_flag.go) with `--quiet` defaulted to `false`, since a debug command's headers are the point. See [CLI](CLI.md#standard-output-flags) for the full table of flags and defaults.

Three behaviours apply to every command on this page:

- **`--format=json` prints the envelope document and nothing else** — no headers, no banners, no trailing prose, and on a single line — so `app debug:router --format=json | jq '.data'` works directly and a stream of documents can be read line by line. `--format=json-pretty` is the same document indented for reading by hand. Selecting json also **implies `--no-color`**: [`NormalizeOption`](../../cli/output/option_parser.go) forces it, so passing `--no-color=false` alongside does not put ANSI escapes back into the document.
- **A command whose envelope carries an error exits non-zero.** [`Render`](../../cli/output/renderer.go) writes the envelope and then returns an exit error with code `1` when `Envelope.Error` is set, so `app debug:container app.repository.order || exit 1` fails a deployment gate on a service that does not resolve, instead of passing because the command "ran". The error travels unmarked, so the exit path also writes it to the application log — the rendered envelope is the report a shell sees, the log record the one a deployment keeps.
- **A route attribute the encoder cannot represent degrades instead of emptying the document.** Route attributes are arbitrary `any` from userland — the `attributes` argument of [`NewRouteOptions`](../../http/route_option.go), handed to the router through `HandleWithOptions` — and one unserializable value used to make the whole `debug:router --format=json` envelope fail to marshal, leaving zero bytes on stdout. A value that marshals keeps its json type; only one that cannot degrades to its `%v` rendering, which names the attribute rather than losing the report — after a cycle-guarded walk has replaced any self-referencing value with a marker, because `fmt` has no cycle detection and a route attribute pointing at itself would otherwise kill the process instead of degrading.
- **`--format` and `--order` reject an unrecognised value** at argument-parsing time (`unsupported output format "…", expected "table", "json" or "json-pretty"`; `unsupported sort order "…", expected "asc" or "desc"`) rather than silently falling back to the default.

## Exported API

### Commands

- [`ContainerCommand`](../../debug/command_container.go)
- [`EventCommand`](../../debug/command_event.go)
    - [`NewEventCommand(deferredListenerProvider DeferredListenerProvider) *EventCommand`](../../debug/command_event.go)
- [`DeferredListener`](../../debug/command_event.go)
- [`DeferredListenerProvider`](../../debug/command_event.go)
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

    "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/debug"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
)

func main() {
    ctx := context.Background()

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    runtimeInstance := runtime.New(
        ctx,
        scope,
        serviceContainer,
    )

    commandContext := cli.NewCommandContext(
        "example",
        "example application",
    )

    /* Register returns nothing: it fails fast through exception.Panic on a nil argument, an empty name or a duplicate name */
    cli.Register(commandContext, &debug.ContainerCommand{}, runtimeInstance)
    /* the zero value leaves the deferred-listener provider nil, so data.servingProcessListeners is omitted; debug.NewEventCommand(provider) is what fills it, and is what the framework's own wiring uses */
    cli.Register(commandContext, &debug.EventCommand{}, runtimeInstance)
    cli.Register(commandContext, &debug.ParameterCommand{}, runtimeInstance)
    cli.Register(commandContext, &debug.RouterCommand{}, runtimeInstance)

    cli.Register(
        commandContext,
        debug.NewMiddlewareCommand(
            func() []httpcontract.Middleware {
                return []httpcontract.Middleware{}
            },
        ),
        runtimeInstance,
    )

    cli.Register(
        commandContext,
        &debug.VersionCommand{ApplicationVersion: "v1.0.0"},
        runtimeInstance,
    )

    /* the registered command's action closes runtimeInstance.Scope() and runtimeInstance.Container() after it runs, so there is nothing to shut down here */
    runErr := commandContext.Run(
        ctx,
        []string{"example", "debug:version"},
    )
    if nil != runErr {
        exception.Panic(
            exception.NewError(
                "debug command run failed",
                nil,
                runErr,
            ),
        )
    }
}

var _ clicontract.Command = (*debug.ContainerCommand)(nil)
```
