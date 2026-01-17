# CLI

The [`cli`](../../cli) package provides core primitives for Melody's command-line integration: command contracts, root command wiring, and shared output/styling helpers.

## Subpackages

- [`cli/contract`](../../cli/contract)  
  Public contracts for CLI commands and flag definitions.

- [`cli/output`](../../cli/output)  
  Output helpers (flags/options, printers, table rendering, and structured envelopes) used by commands.

## Responsibilities

- Define the `clicontract.Command` interface used by Melody to integrate userland commands.
- Provide `cli.NewCommandContext(...)` to create the root command.
- Provide `cli.Register(...)` to register a command (including deterministic name validation and runtime-aware shutdown).
- Expose shared ANSI styling constants for consistent CLI output.

## Exported API

### Contracts (`cli/contract`)

- [`clicontract.Command`](../../cli/contract/command.go)
- [`clicontract.CommandContext`](../../cli/contract/type.go) (alias)
- [`clicontract.Flag`](../../cli/contract/type.go) (alias)
- [`clicontract.StringFlag`](../../cli/contract/type.go) (alias)
- [`clicontract.BoolFlag`](../../cli/contract/type.go) (alias)
- [`clicontract.IntFlag`](../../cli/contract/type.go) (alias)

### Root command wiring (`cli`)

- [`cli.NewCommandContext(applicationName string, applicationDescription string) *clicontract.CommandContext`](../../cli/command.go)
- [`cli.Register(commandContext *clicontract.CommandContext, command clicontract.Command, runtimeInstance runtimecontract.Runtime)`](../../cli/command.go)

### ANSI styling (`cli`)

- [`cli.AnsiReset`](../../cli/style.go)
- [`cli.AnsiBold`](../../cli/style.go)
- [`cli.AnsiCyan`](../../cli/style.go)
- [`cli.AnsiGreen`](../../cli/style.go)
- [`cli.AnsiYellow`](../../cli/style.go)
- [`cli.AnsiRed`](../../cli/style.go)
- [`cli.AnsiBackgroundGreen`](../../cli/style.go)
- [`cli.AnsiWhite`](../../cli/style.go)
- [`cli.AnsiEraseLine`](../../cli/style.go)

### Output helpers (`cli/output`)

This subpackage provides shared helpers that commands can use for consistent output formatting.

- Flags and options:
    - [`output.StandardFlags()`](../../cli/output/standard_flag.go)
    - [`output.DebugFlags()`](../../cli/output/standard_flag.go)
    - [`output.ParseOptionFromCommand(...)`](../../cli/output/option_parser.go)
    - [`output.NormalizeOption(option output.Option) output.Option`](../../cli/output/option_parser.go)

- Printing and rendering:
    - [`output.Printer`](../../cli/output/printer.go)
    - [`output.Render(...)`](../../cli/output/renderer.go)
    - [`output.SelectPrinter(option output.Option) output.Printer`](../../cli/output/printer_selector.go)

- Table output:
    - [`output.NewTableBuilder() *output.TableBuilder`](../../cli/output/table_builder.go)
    - [`output.NewDefaultTablePrinter() *output.TablePrinter`](../../cli/output/table_printer.go)

- Structured envelopes:
    - [`output.NewEnvelope(...)`](../../cli/output/envelope_factory.go)
    - [`output.Envelope`](../../cli/output/envelope.go)

## Usage

```go
package main

import (
    "context"

    "github.com/precision-soft/melody/cli"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type HelloCommand struct{}

func (instance *HelloCommand) Name() string {
    return "example:hello"
}

func (instance *HelloCommand) Description() string {
    return "prints a hello message"
}

func (instance *HelloCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{}
}

func (instance *HelloCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    writer := commandContext.Writer
    if nil == writer {
        return nil
    }

    _, _ = writer.Write([]byte("hello\n"))

    return nil
}

func main() {
    ctx := context.Background()

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    runtimeInstance := runtime.New(
        ctx,
        scope,
        serviceContainer,
    )

    rootCli := cli.NewCommandContext(
        "example",
        "example application",
    )

    cli.Register(rootCli, &HelloCommand{}, runtimeInstance)

    runErr := rootCli.Run(ctx, []string{"example", "example:hello"})
    if nil != runErr {
        exception.Panic(
            exception.NewError(
                "cli run failed",
                nil,
                runErr,
            ),
        )
    }
}
```

## Footguns & caveats

- `cli.Register(...)` fails fast via the [`exception`](../../exception) package if the root command context, command, or runtime instance is nil.
- Command names are normalized using `strings.TrimSpace(...)`. Empty names and duplicates are rejected.
- Registered command execution will close `runtimeInstance.Scope()` and `runtimeInstance.Container()` after `Run(...)` and may return aggregated shutdown errors.
  EOF