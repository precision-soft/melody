# CLI

The [`cli`](../../cli) package provides core primitives for Melody's command-line integration: command contracts, root command wiring, and shared output/styling helpers.

## Subpackages

- [`cli/contract`](../../cli/contract)  
  Public contracts for CLI commands and flag definitions.

- [`cli/output`](../../cli/output)  
  Output helpers (flags/options, printers, table rendering, and structured envelopes) used by commands.

> Looking for a crontab generator? The cron integration lives in its own module: see [`integrations/cron/`](../../integrations/cron/) (use the root binding — `github.com/precision-soft/melody/integrations/cron` — for Melody v1).

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
- [`clicontract.StringSliceFlag`](../../cli/contract/type.go) (alias)
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

- Flag names (string constants):
    - [`FlagNameFormat`, `FlagNameNoColor`, `FlagNameVerbose`, `FlagNameVerbosity`, `FlagNameQuiet`, `FlagNameOrder`, `FlagNameLimit`, `FlagNameOffset`, `FlagNameTableMaxWidth`](../../cli/output/flag.go)
    - [`output.MergeFlags(flagSets ...[]clicontract.Flag) []clicontract.Flag`](../../cli/output/flag.go)

- Output format and ordering:
    - [`type Format`](../../cli/output/format.go) with constants [`FormatTable`, `FormatJson`](../../cli/output/format.go)
    - [`type SortOrder`](../../cli/output/format.go) with constants [`SortOrderAscending`, `SortOrderDescending`](../../cli/output/format.go)

- Flags and options:
    - [`output.StandardFlags()`](../../cli/output/standard_flag.go)
    - [`output.DebugFlags()`](../../cli/output/standard_flag.go)
    - [`output.ParseOptionFromCommand(...)`](../../cli/output/option_parser.go)
    - [`output.NormalizeOption(option output.Option) output.Option`](../../cli/output/option_parser.go)
    - [`type Option`](../../cli/output/option.go)

- Printing and rendering:
    - [`output.Printer`](../../cli/output/printer.go)
    - [`output.Render(...)`](../../cli/output/renderer.go) — prints the envelope and then returns an **exit-coded error** when the envelope carries an error, so a failing command leaves the process with a non-zero status and a shell gate such as `app debug:container app.missing || exit 1` holds. That error is pre-marked as logged, since the rendered envelope already carries the full report; a printing failure is still returned as a plain error.
    - [`output.SelectPrinter(option output.Option) output.Printer`](../../cli/output/printer_selector.go)

- List payloads:
    - [`output.NewListPayload(items []T, total int, limit int, offset int) output.ListPayload[T]`](../../cli/output/list_payload.go)
    - [`output.WindowItems(items []T, limit int, offset int) []T`](../../cli/output/list_payload.go) — the shared `--limit`/`--offset` window over an already-ordered slice, so a command never bounds-checks the flags itself: a non-positive limit means "to the end", a negative offset is clamped to zero, and an offset past the end yields an empty window. Report `Total` from the full slice and `Items` from the window.

- Table output:
    - [`output.NewTableBuilder() *output.TableBuilder`](../../cli/output/table_builder.go)
    - [`output.NewTablePrinter(tableMaxWidth int) *output.TablePrinter`](../../cli/output/table_printer.go)
    - [`output.NewDefaultTablePrinter() *output.TablePrinter`](../../cli/output/table_printer.go)

- Structured envelopes:
    - [`output.NewEnvelope(...)`](../../cli/output/envelope_factory.go)
    - [`output.NewMeta(...)`](../../cli/output/envelope_factory.go)
    - [`output.NewWarning(code, message, details)`](../../cli/output/envelope_factory.go)
    - [`output.NewError(code, message, details, cause)`](../../cli/output/envelope_factory.go)
    - [`output.NewErrorCause(message, details)`](../../cli/output/envelope_factory.go)
    - [`output.NewListPayload[T](...)`](../../cli/output/list_payload.go)
    - [`output.Envelope`](../../cli/output/envelope.go)

- Application version:
    - [`output.SetApplicationVersion(versionString string)`](../../cli/output/application_version.go)

### Standard output flags

[`output.StandardFlags()`](../../cli/output/standard_flag.go) is the shared flag set for a command that renders an envelope. [`output.DebugFlags()`](../../cli/output/standard_flag.go) returns the same flags with `--quiet` defaulted to `false` instead of `true`; the flag set is otherwise identical.

| Flag             | Type   | Default (`StandardFlags`)             |
|------------------|--------|---------------------------------------|
| `--format`       | string | `table`                               |
| `--no-color`     | bool   | `false`                               |
| `--verbose`      | bool   | `false`                               |
| `--verbosity`    | int    | `0` (accepts `-v`/`-vv`/`-vvv` through argument normalization) |
| `--quiet`        | bool   | `true` (`false` under `DebugFlags`)   |
| `--order`        | string | `asc`                                 |
| `--limit`        | int    | `0` (unlimited)                       |
| `--offset`       | int    | `0`                                   |
| `--table-width`  | int    | `0` (built-in default)                |

Note the flag is spelled `--table-width`, though its constant is `FlagNameTableMaxWidth`.

Two behaviours are worth knowing before wiring a command against these, alongside the exit-code rule noted on `output.Render` above:

* **`--format=json` emits the envelope document and nothing else.** [`JsonPrinter`](../../cli/output/json_printer.go) encodes the `Envelope` as indented JSON and writes no headers, banners or trailing prose, so the output pipes straight into `jq`. Selecting it also **implies `--no-color`**: [`NormalizeOption`](../../cli/output/option_parser.go) forces `NoColor` on for the json format, because a single machine-readable document must not carry ANSI escapes — passing `--no-color=false` alongside `--format=json` does not put them back.
* **`--format` and `--order` reject an unrecognised value.** Both carry a flag `Validator`, so argument parsing fails with `unsupported output format "…", expected "table" or "json"` (respectively `unsupported sort order "…", expected "asc" or "desc"`) instead of quietly using the default. [`NormalizeOption`](../../cli/output/option_parser.go) *does* coerce an unsupported value to the default, but that is a defensive floor for an `Option` assembled in code, not the path a command-line argument takes.

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