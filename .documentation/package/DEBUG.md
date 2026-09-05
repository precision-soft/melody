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

`debug:parameters` reports a `secret` column: a parameter declared as holding a credential (see [CONFIG](CONFIG.md#secret-parameters)) renders its value as `********` — or `(empty)` when it carries none, including a nil value — with the length withheld, while an ordinary parameter prints in the clear.

`debug:container` and `debug:middleware` describe by default and build only on request. The bare `debug:container` listing runs no provider: it reports names, lifetimes, built state and the provider's declared return type from the registration records, and the resolve sweep that builds services sits behind `--build`; naming a single service still resolves it. The bare `debug:middleware` listing describes the pipeline — the selection and ordering the build would run, with the inactive entries carrying their reasons — and only `--build` runs the real factories, under a recover so a panicking factory renders as a failure with its cause instead of killing the command.

The `--build` sweep reports its failures on the envelope, so it fails a deployment gate the way the single-service door always has: `app debug:container --build --format=json || exit 1` exits non-zero when any windowed service does not resolve, with `error.code = "debug.buildFailed"`, the failure count and the failed names in the details, and the first failure as the cause. `errorCauseChain` and `errorContextJson` keep one json type across every row of one document — an empty array and `{}` on a service that resolved — so `jq '.data.items[].errorCauseChain[]'` and `.errorContextJson | fromjson` work on the whole listing; the table keeps the empty cell.

**The `debug:*` family is registered on the development environment alone**, `debug:router` excepted — [`bootCli`](../../application/application_cli.go) appends the rest only when `Kernel().Env()` is `EnvDevelopment`. A deployed application therefore does not have `debug:container` at all: the invocation falls to the unknown-command path, which exits `2` with `cli command not found`. A shell gate written as `|| exit 1` still fires there, but for the wrong reason — it is reporting a missing command, not a failing service. A gate that must exercise the container has to run against a process booted in the development environment.

`debug:container` reports why a failing service failed, not only that it did: the causes below the resolution error render as `caused by` lines in the table views, as `errorCauseChain` in the json items, and on the envelope error's cause details. The error context is read through the `ContextProvider` contract, so an `HttpException` or a userland context-bearing error contributes its context too. The rendered context is redacted by key name — keys containing `trace` or `stack` are dropped, case-insensitively, while full verbosity (`-vvv`, verbosity level three and above) keeps the whole context — which is a diagnostic-noise filter, deliberately not a credential filter: an error context has no secret declaration the way parameters do, so keeping credentials out of error contexts is the producer's responsibility. The context json is truncated to the table-cell width only in the table format; the json document always carries it whole.

`debug:events --format=json --verbose` carries the per-listener detail — priority, source, owner and the required / may-skip marks — under `data.listeners`, beside the event list under `data.events`; without `--verbose` the json `data` stays the plain list payload. The **declaration** of the listeners a serving process wires and this one does not is on `data.servingProcessListeners` at every verbosity, beside the listing rather than instead of it, because it exists so that "is access control wired?" is not answered with an absence meaning "not in this process". `--order` reaches both halves of the document: it orders the events, while inside one event the listener rows keep the dispatch order their `order` field reports. The table summary's `SUBSCRIBERS` total counts distinct subscribers across the whole dispatcher, while the per-event column counts them per event.

The `debug:container` list summary orders its segments `total | shown | ok | error` when a window is applied: only the windowed services are resolved, so the ok/error split answers the shown rows, not the total.

## Flags and output

Every debug command declares [`output.DebugFlags()`](../../cli/output/standard_flag.go) as its flag set — `debug:container` and `debug:middleware` appending their own `--build` beside it — and renders its result through [`output.Render`](../../cli/output/renderer.go), so all six share one interface. `DebugFlags()` is [`output.StandardFlags()`](../../cli/output/standard_flag.go) with `--quiet` defaulted to `false`, since a debug command's headers are the point. See [CLI](CLI.md#standard-output-flags) for the full table of flags and defaults.

Shared behaviours — the first, second and last applying to every command on this page, the middle two to the command each one names:

- **`--format=json` prints the envelope document and nothing else** — no headers, no banners, no trailing prose, and on a single line — so `app debug:router --format=json | jq '.data'` works directly and a stream of documents can be read line by line. `--format=json-pretty` is the same document indented for reading by hand. Selecting json also **implies `--no-color`**: [`NormalizeOption`](../../cli/output/option_parser.go) forces it, so passing `--no-color=false` alongside does not put ANSI escapes back into the document.
- **A command whose envelope carries an error exits non-zero.** [`Render`](../../cli/output/renderer.go) writes the envelope and then returns an exit error with code `1` when `Envelope.Error` is set, so `app debug:container app.repository.order || exit 1` fails a deployment gate on a service that does not resolve, instead of passing because the command "ran". The error travels unmarked, so the exit path also writes it to the application log — the rendered envelope is the report a shell sees, the log record the one a deployment keeps.
- **A route attribute the encoder cannot represent degrades instead of emptying the document.** Route attributes are arbitrary `any` from userland — the `attributes` argument of [`NewRouteOptions`](../../http/route_option.go), handed to the router through `HandleWithOptions` — and one unserializable value used to make the whole `debug:router --format=json` envelope fail to marshal, leaving zero bytes on stdout. A value that marshals keeps its json type; only one that cannot degrades to its `%v` rendering, which names the attribute rather than losing the report — after a cycle-guarded walk has replaced any self-referencing value with a marker, because `fmt` has no cycle detection and a route attribute pointing at itself would otherwise kill the process instead of degrading.
- **`debug:middleware` items always carry `reason`**, empty where there is nothing to say. `--build` cannot fill `name` and `priority`: the build provider hands back the built chain alone, and correlating it with the described pipeline by position would be a guess, since the description also lists the inactive entries the build never produces. Read the default listing — which describes without building — when the name is what you need.
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

- [`NewMiddlewareCommand(descriptionProvider MiddlewareDescriptionProvider, buildProvider MiddlewareBuildProvider) *MiddlewareCommand`](../../debug/command_middleware.go) — panics on a nil provider; return empty results from the providers when there is nothing to list
- [`MiddlewareDescriptionProvider`](../../debug/command_middleware.go)
- [`MiddlewareBuildProvider`](../../debug/command_middleware.go)

## Usage

```go
package main

import (
    "context"

    "github.com/precision-soft/melody/cli"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/debug"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/http/middleware/pipeline"
    "github.com/precision-soft/melody/runtime"
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
            func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
                return []middlewarepipeline.MiddlewareDescription{}, nil, nil
            },
            func() ([]httpcontract.Middleware, error) {
                return []httpcontract.Middleware{}, nil
            },
        ),
        runtimeInstance,
    )

    cli.Register(
        commandContext,
        &debug.VersionCommand{ApplicationVersion: "v1.0.0"},
        runtimeInstance,
    )

    /* the registered command's action closes runtimeInstance.Scope() after it runs, and the exit handler closes the container, so there is nothing to shut down here */
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
