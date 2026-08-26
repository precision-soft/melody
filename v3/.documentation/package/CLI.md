# CLI

The [`cli`](../../cli) package provides core primitives for Melody's command-line integration: command contracts, root command wiring, and shared output/styling helpers.

## Subpackages

- [`cli/contract`](../../cli/contract)  
  Public contracts for CLI commands and flag definitions.

- [`cli/output`](../../cli/output)  
  Output helpers (flags/options, printers, table rendering, and structured envelopes) used by commands.

> Looking for a crontab generator? The cron integration lives in its own module: see [`integrations/cron/`](../../../integrations/cron/) (use the [`v3`](../../../integrations/cron/v3/) binding).

## Responsibilities

- Define the `clicontract.Command` interface used by Melody to integrate userland commands.
- Own the flag and context layer a command is written against, so the flag-parsing engine behind it stays an implementation detail of this package.
- Provide `cli.NewRoot(...)` to create the command tree.
- Provide `cli.Register(...)` to register a command (including deterministic name validation and runtime-aware shutdown).
- Provide `cli.DispatchCommand(...)` for a caller that runs one command inside a process it owns.
- Expose shared ANSI styling constants for consistent CLI output.

## Exported API

### Contracts (`cli/contract`)

- [`clicontract.Command`](../../cli/contract/command.go)
- [`clicontract.Context`](../../cli/contract/context.go) — what a command's `Run` receives: `String`, `Bool`, `Int`, `StringSlice`, `IsSet`, `Arguments`, `Writer`
- [`clicontract.StaticContext`](../../cli/contract/static_context.go) — a `Context` whose answers are given rather than parsed, for testing a command's body without a command line
- [`clicontract.Flag`](../../cli/contract/flag.go) — one method, `Definition() FlagDefinition`
- [`clicontract.FlagDefinition`](../../cli/contract/flag.go)
- [`clicontract.FlagKind`](../../cli/contract/flag.go) with constants [`FlagKindString`, `FlagKindBool`, `FlagKindInt`, `FlagKindStringSlice`](../../cli/contract/flag.go)
- [`clicontract.ErrFlagValueTypeMismatch`](../../cli/contract/flag.go)
- [`clicontract.StringFlag`](../../cli/contract/flag.go)
- [`clicontract.StringSliceFlag`](../../cli/contract/flag.go)
- [`clicontract.BoolFlag`](../../cli/contract/flag.go)
- [`clicontract.IntFlag`](../../cli/contract/flag.go)

### Root command wiring (`cli`)

- [`cli.Root`](../../cli/root.go)
- [`cli.NewRoot(applicationName string, applicationDescription string) *cli.Root`](../../cli/root.go)
- [`cli.Root.SetWriter(writer io.Writer)`](../../cli/root.go)
- [`cli.Root.SetErrorWriter(writer io.Writer)`](../../cli/root.go)
- [`cli.Root.Run(ctx context.Context, arguments []string) error`](../../cli/root.go)
- [`cli.Root.CommandNames() []string`](../../cli/root.go)
- [`cli.Register(root *cli.Root, command clicontract.Command, runtimeInstance runtimecontract.Runtime)`](../../cli/command.go)
- [`cli.DispatchCommand(ctx context.Context, command clicontract.Command, runtimeInstance runtimecontract.Runtime, arguments []string, writer io.Writer) error`](../../cli/dispatch.go)

### The flag and context layer

The types above are **Melody's own**, and the flag-parsing engine — [`github.com/urfave/cli/v3`](https://github.com/urfave/cli) — is reached exclusively through [`cli/adapter.go`](../../cli/adapter.go), the one source in the module that names it. This is the layer the first two majors documented as a v3 change and could not make: there `CommandContext` and the flag types are **type aliases** of the engine's own structs, which puts every field that engine's command struct has, mutable, into the major's public surface and makes the engine's API part of its compatibility promise.

What it buys, concretely: a command reads the seven values it actually needs instead of holding the engine's command object; an engine release that renames or removes a method reaches consumers only if and when this package's adapter passes it on; and a command's body can be tested against `clicontract.StaticContext` rather than by driving an argv through a parser. What it costs: only the four flag kinds above exist. A package that needs its own flag type implements `clicontract.Flag` over one of those kinds — a port number that validates its range, a path that must exist — rather than reaching for an engine flag melody does not carry.

A flag kind the adapter cannot build, and a declared default whose type does not match its kind, are refused with a panic where the command is registered: a flag that cannot be built is a wiring mistake, and the alternative is a command whose flag silently does not exist.

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

	"github.com/precision-soft/melody/v3/cli"
	clicontract "github.com/precision-soft/melody/v3/cli/contract"
	"github.com/precision-soft/melody/v3/container"
	"github.com/precision-soft/melody/v3/exception"
	"github.com/precision-soft/melody/v3/runtime"
	runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func (instance *HelloCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
	_, _ = commandContext.Writer().Write([]byte("hello\n"))

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

	rootCli := cli.NewRoot(
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

- `cli.Register(...)` fails fast via the [`exception`](../../exception) package if the root, command, or runtime instance is nil.
- Command names are normalized using `strings.TrimSpace(...)`. Empty names and duplicates are rejected.
- `cli.Root` installs an inert exit handler on the engine and offers no door to remove it: left at its default the engine ends the process itself, from inside `Run`, on any error a command returns — and Melody owns the exit, so the application's deferred `Close` and its structured error record would never run.
- `Root.SetWriter` and `Root.SetErrorWriter` reach the commands too, whenever they were registered. The engine defaults each command's stream separately, to the process's standard output, so a writer set on the tree alone would leave every command writing past it.
- `cli.DispatchCommand(...)` adds none of what `Register` adds around a command — no banner, no scope close, no exit handling — and is for a caller that runs one command inside a process it owns, such as a scheduler. One engine behaviour travels with its `ctx`: a command dispatched with a context that **descends from another command's action** inherits that command's flag set, so an argument naming a flag only the outer command declares is accepted rather than refused. Dispatch from the process's own context, which is what every caller inside Melody does, and the command is parsed against its own flags alone.
- Registered command execution will close `runtimeInstance.Scope()` and `runtimeInstance.Container()` after `Run(...)` and may return aggregated shutdown errors. EOF