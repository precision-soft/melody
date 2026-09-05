# CLI

The [`cli`](../../cli) package provides core primitives for Melody's command-line integration: command contracts, root command wiring, and shared output/styling helpers.

## Subpackages

- [`cli/contract`](../../cli/contract)  
  Public contracts for CLI commands and flag definitions.

- [`cli/output`](../../cli/output)  
  Output helpers (flags/options, printers, table rendering, and structured envelopes) used by commands.

> Looking for a crontab generator? The cron integration lives in its own module: see [`integrations/cron/`](../../../integrations/cron/) (use the [`v2`](../../../integrations/cron/v2/) binding).

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

The flag and context types are **type aliases of [`github.com/urfave/cli/v3`](https://github.com/urfave/cli)**: `CommandContext` is that library's `Command`, the runtime value every command's `Run` receives, and the flag types are its flag structs. This pins v2's CLI surface to `urfave/cli/v3` for the life of the major — a command reads flag values through the urfave context, and a flag is declared as an urfave struct. The coupling is deliberate for v2 (it ships the whole flag-parsing engine without a wrapper); a melody-owned flag and context layer that would let the engine be swapped is a v3 change, not a v2 one, since introducing it here would cascade a signature change through every command of the framework, the integrations and the example.

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
- [`cli.AnsiBackgroundRed`](../../cli/style.go)
- [`cli.AnsiWhite`](../../cli/style.go)
- [`cli.AnsiEraseLine`](../../cli/style.go)

### Output helpers (`cli/output`)

This subpackage provides shared helpers that commands can use for consistent output formatting.

- Flag names (string constants):
    - [`FlagNameFormat`, `FlagNameNoColor`, `FlagNameVerbose`, `FlagNameVerbosity`, `FlagNameQuiet`, `FlagNameOrder`, `FlagNameLimit`, `FlagNameOffset`, `FlagNameTableMaxWidth`](../../cli/output/flag.go)
    - [`output.MergeFlags(standard []clicontract.Flag, commandSpecific []clicontract.Flag) []clicontract.Flag`](../../cli/output/flag.go) — concatenates the two sets and **panics on a duplicated flag name**, and on a nil flag: the parser resolves a name to the first declaration, so a command-specific flag reusing a standard name would be silently inert. Do not reuse the `FlagName*` names.

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
    - [`output.Printer`](../../cli/output/printer.go) — the interface `SelectPrinter` answers with, exported so a caller can hold what it returns. **The set of formats is closed**: `table`, `json` and `json-pretty`, decided by [`isFormatSupported`](../../cli/output/format.go) and [`SelectPrinter`](../../cli/output/printer_selector.go), with no registration door. Implementing `Printer` in userland therefore gets a type that nothing dispatches to — `--format` refuses any value the two functions above do not know, before a command runs. This is deliberate for now, and it is the one exported interface of the framework that is not an extension point: the envelope, the flag set and the two renderings are the contract every melody command shares, and a fourth rendering chosen by an operator would make `--format=json` mean something different per application. A command that needs its own rendering writes it inside the command, from the envelope it already holds.
    - [`output.Render(...)`](../../cli/output/renderer.go) — prints the envelope and then returns an **exit-coded error** when the envelope carries an error, so a failing command leaves the process with a non-zero status and a shell gate such as `app debug:container app.missing || exit 1` holds. The error travels **unmarked**, so the exit path also writes it to the application log — the rendered report lives only on the output streams. A printing failure is returned with the envelope's own failure preserved as its cause, never in its place.
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

| Flag            | Type   | Default (`StandardFlags`)                                      |
|-----------------|--------|----------------------------------------------------------------|
| `--format`      | string | `table` (`table`, `json`, `json-pretty`)                       |
| `--no-color`    | bool   | `false`                                                        |
| `--verbose`     | bool   | `false`                                                        |
| `--verbosity`   | int    | `0` (accepts `-v`/`-vv`/`-vvv` through argument normalization) |
| `--quiet`       | bool   | `true` (`false` under `DebugFlags`)                            |
| `--order`       | string | `asc`                                                          |
| `--limit`       | int    | `0` (unlimited)                                                |
| `--offset`      | int    | `0`                                                            |
| `--table-width` | int    | `0` (built-in default)                                         |

Note the flag is spelled `--table-width`, though its constant is `FlagNameTableMaxWidth`.

The four integer flags refuse a negative value at parsing, naming the flag — a negative used to be clamped to zero, and zero means unlimited for the limit and "from the start" for the offset, so an argument asking for less than nothing silently delivered everything. The clamp in `NormalizeOption` stays as the defensive floor for an `Option` assembled in code.

Four behaviours are worth knowing before wiring a command against these, alongside the exit-code rule noted on `output.Render` above:

* **`--format=json` emits the envelope document and nothing else, on ONE line.** [`JsonPrinter`](../../cli/output/json_printer.go) encodes the `Envelope` compactly, terminated by a newline, and writes no headers, banners or trailing prose, so the output pipes straight into `jq` — and a long-running command that renders a document per unit of work is a stream a line reader can follow, handing each line to a parser whole. Use `--format=json-pretty` for the same document indented for reading by hand, or `| jq` where the pipeline already has it; the two formats differ in whitespace alone and every rule below holds for both. Selecting it also **implies `--no-color`**: [`NormalizeOption`](../../cli/output/option_parser.go) forces `NoColor` on for the json format, because a single machine-readable document must not carry ANSI escapes — passing `--no-color=false` alongside `--format=json` does not put them back.
* **`--format` and `--order` reject an unrecognised value.** Both carry a flag `Validator`, so argument parsing fails with `unsupported output format "…", expected "table", "json" or "json-pretty"` (respectively `unsupported sort order "…", expected "asc" or "desc"`) instead of quietly using the default. [`NormalizeOption`](../../cli/output/option_parser.go) *does* coerce an unsupported value to the default, but that is a defensive floor for an `Option` assembled in code, not the path a command-line argument takes.
* **`--quiet` suppresses the headers, never the warnings or the error.** The table printer renders the `WARNINGS` block and the envelope error (message, code, details, cause) under quiet as well — they are what the command said beside its result; only the warning *details* stay behind `--verbose`. The json document has always carried both.
* **The `-v`/`-vv`/`-vvv` normalization rewrites tokens, not grammar.** Every standalone argv token of that exact shape becomes `--verbosity=N`, before the parser knows whether the token was meant as the *value* of the preceding flag — `some:cmd --pattern -vv` hands `--pattern` the rewritten token. Write such a value in the attached form (`--pattern=-vv`) or after the `--` terminator, which stops the normalization.

## Usage

```go
package main

import (
	"context"

	"github.com/precision-soft/melody/v2/cli"
	clicontract "github.com/precision-soft/melody/v2/cli/contract"
	"github.com/precision-soft/melody/v2/container"
	"github.com/precision-soft/melody/v2/exception"
	"github.com/precision-soft/melody/v2/runtime"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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
- Registered command execution closes `runtimeInstance.Scope()` after `Run(...)` and may fold that close's failure into the command's result; the container is deliberately left open on either outcome — the recover handler that owns the exit resolves the final record's logger through it and closes it between the record and `os.Exit`. On a panic the finish banner reports `[failed]` before the panic is re-raised unchanged.
- A table row must match its block: `AddRow(...)` panics on a row whose cell count disagrees with the block's declared columns; the single-token separator row (`TableRowSeparatorToken`) is the one exception.
- The table builder and the envelope are not safe for concurrent use: a command assembling its table or warnings from parallel work funnels them through one goroutine.
- The banners honour `--no-color`, but not uniformly: the status lines degrade to plain text, while the full-width coloured rules that frame them are **omitted entirely** — they are nothing but colour, so there is no plain text for them to become. A `--no-color` run therefore shows the status lines alone, without the frame. Under `--format=json` the whole banner is suppressed so the document stays parseable. The banner is decoration under the `--quiet` contract as well: quiet suppresses it entirely, so a `StandardFlags` command's default invocation prints its own output alone and `--quiet=false` brings the frame back, while a `DebugFlags` command keeps its banner by default.
