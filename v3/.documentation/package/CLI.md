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
- Provide `cli.NewCommandContext(...)` to create the root command, and `cli.NewRoot(...)` for a command tree that owns its command list.
- Provide `cli.Register(...)` and `Root.Register(...)` to register a command (including deterministic name validation and runtime-aware shutdown).
- Provide `cli.DispatchCommand(...)` for a caller that runs one command inside a process it owns.
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
- [`cli.Root`](../../cli/root.go)
- [`cli.NewRoot(applicationName string, applicationDescription string) *cli.Root`](../../cli/root.go)
- [`cli.Root.Register(command clicontract.Command, runtimeInstance runtimecontract.Runtime)`](../../cli/root.go)
- [`cli.Root.SetWriter(writer io.Writer)`](../../cli/root.go)
- [`cli.Root.SetErrorWriter(writer io.Writer)`](../../cli/root.go)
- [`cli.Root.Run(ctx context.Context, arguments []string) error`](../../cli/root.go)
- [`cli.Root.CommandNames() []string`](../../cli/root.go)
- [`cli.DispatchCommand(ctx context.Context, command clicontract.Command, runtimeInstance runtimecontract.Runtime, arguments []string, writer io.Writer) error`](../../cli/dispatch.go)

### The flag and context layer

The flag and context types are **type aliases of [`github.com/urfave/cli/v3`](https://github.com/urfave/cli)**: `CommandContext` is that library's `Command`, and a command reads its flags by name (`String`, `Bool`, `Int`, `StringSlice`, `IsSet`), its positional arguments through `Args().Slice()` and its output stream through the `Writer` field. A flag is one of the engine's own flag structs, so `Required`, `Aliases`, `Hidden`, a typed `Validator` and the engine's other fields are declared on it directly. The engine writes parse state into the flag instances it is handed, so a command's `Flags()` builds fresh instances on every call.

The four standard integer flags of `output.StandardFlags()` parse in base ten: `--limit=010` is ten, `--limit=08` is eight and `--limit=0x10` is refused. A command's own integer flag declares its own base; left unset, the engine infers it from the literal.

A spelling a command declares twice — an alias repeating another flag's name, two flags sharing an alias — an empty alias and a nil flag are refused with a panic where the command is registered: the parser resolves a spelling to the first flag declaring it, so the second would be parsed as the first with nothing said. The engine's own help flag, `help` with its alias `h`, is on every command, so a flag spelled either way is refused the same.

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
    - [`FlagNameFormat`, `FlagNameNoColor`, `FlagNameVerbose`, `FlagNameVerbosity`, `FlagNameQuiet`, `FlagNameOrder`, `FlagNameLimit`, `FlagNameOffset`, `FlagNameTableMaxWidth`](../../cli/output/flag.go), and the deprecated [`FlagNameFields`, `FlagNameSortKey`](../../cli/output/flag.go) with [`output.SplitFields(fieldsString string) []string`](../../cli/output/standard_flag.go)
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
    - [`output.Render(...)`](../../cli/output/renderer.go) — prints the envelope and then returns an **exit-coded error** when the envelope carries an error, so a failing command leaves the process with a non-zero status and a shell gate such as `app debug:container app.missing || exit 1` holds. The error travels **unmarked**, so the exit path also writes it to the application log — the rendered report lives only on the output streams. A printing failure is returned with the envelope's own failure preserved as its cause, never in its place; a write the sink accepted only in part, with no error, is a printing failure too, remembered as `io.ErrShortWrite` — the sink is the application's, and a report it truncated must not end under a success banner.
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
| `--fields`      | string | `""` (deprecated: parsed into `Option.Fields`, read by no printer) |
| `--sort`        | string | `""` (deprecated: parsed into `Option.SortKey`, read by no printer) |
| `--order`       | string | `asc`                                                          |
| `--limit`       | int    | `0` (unlimited)                                                |
| `--offset`      | int    | `0`                                                            |
| `--table-width` | int    | `0` (built-in default)                                         |

Note the flag is spelled `--table-width`, though its constant is `FlagNameTableMaxWidth`.

The four integer flags refuse a negative value at parsing, naming the flag — a negative used to be clamped to zero, and zero means unlimited for the limit and "from the start" for the offset, so an argument asking for less than nothing silently delivered everything. The clamp in `NormalizeOption` stays as the defensive floor for an `Option` assembled in code.

Four behaviours are worth knowing before wiring a command against these, alongside the exit-code rule noted on `output.Render` above:

* **`--format=json` emits the envelope document and nothing else, on ONE line.** [`JsonPrinter`](../../cli/output/json_printer.go) encodes the `Envelope` compactly, terminated by a newline, and writes no headers, banners or trailing prose, so the output pipes straight into `jq` — and a long-running command that renders a document per unit of work is a stream a line reader can follow, handing each line to a parser whole. Use `--format=json-pretty` for the same document indented for reading by hand, or `| jq` where the pipeline already has it; the two formats differ in whitespace alone and every rule below holds for both. Selecting it also **implies `--no-color`**: [`NormalizeOption`](../../cli/output/option_parser.go) forces `NoColor` on for the json format, because a single machine-readable document must not carry ANSI escapes — passing `--no-color=false` alongside `--format=json` does not put them back.
* **`--format` and `--order` reject an unrecognised value.** Both carry a flag `Validator`, so argument parsing fails with `unsupported output format "…", expected "table", "json" or "json-pretty"` (respectively `unsupported sort order "…", expected "asc" or "desc"`) instead of quietly using the default. [`NormalizeOption`](../../cli/output/option_parser.go) *does* coerce an unsupported value to the default, but that is a defensive floor for an `Option` assembled in code, not the path a command-line argument takes.
* **`--quiet` suppresses the headers, never the warnings or the error.** The table printer renders the `WARNINGS` block and the envelope error (message, code, details, cause) under quiet as well — they are what the command said beside its result; only the warning *details* stay behind `--verbose`. The json document has always carried both. The run banner [`cli.Register`](../../cli/command.go) wraps around a registered command is decoration under the same contract: quiet suppresses it entirely, so a `StandardFlags` command's default invocation prints its own output alone and `--quiet=false` brings the frame back, while a `DebugFlags` command keeps its banner by default. The `[error]` line a failing command ends with is not part of that frame: it is written whatever quiet says and in every format, since it is the failure's one answer on the terminal, and it goes to the error stream (the command's `ErrWriter`, standard error by default), so a command whose output is a document redirected to a file never ends it with the line. A command that declares no `--quiet` flag reads it as false and keeps the banner; `melody:openapi:generate` and `melody:wiring:generate`, whose output is a document, declare the flag themselves with the `StandardFlags` default, so the document printed to stdout is not wrapped in the frame.
* **The `-v`/`-vv`/`-vvv` normalization rewrites tokens, not grammar.** Every standalone argv token of that exact shape becomes `--verbosity=N`, before the parser knows whether the token was meant as the *value* of the preceding flag — `some:cmd --pattern -vv` hands `--pattern` the rewritten token. Write such a value in the attached form (`--pattern=-vv`) or after the `--` terminator, which stops the normalization. The rewrite is [`cli.NormalizeVerbosityArguments`](../../cli/verbosity.go): the application's console entry and [`cli.DispatchCommand`](../../cli/dispatch.go) both run it, so a command an in-process runner dispatches with `-vv` reads level 2 as it does from a shell. A positional `help` or `h` is the command's own argument; `--help` prints the usage.

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

func (instance *HelloCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
	_, _ = commandContext.Writer.Write([]byte("hello\n"))

	return nil
}

func main() {
	ctx := context.Background()

	serviceContainer := container.NewContainer()
	/* a container built by hand is closed by the hand that built it: there is no exit handler here to close it */
	defer func() {
		_ = serviceContainer.Close()
	}()

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

- `cli.Register(...)` fails fast via the [`exception`](../../exception) package if the root, command, or runtime instance is nil.
- Command names are normalized using `strings.TrimSpace(...)`. Empty names and duplicates are rejected.
- `cli.NewCommandContext(...)` installs an inert exit handler on the engine: left at its default the engine ends the process itself, from inside `Run`, on any error a command returns — and Melody owns the exit, so the application's deferred `Close` and its structured error record would never run. A caller may set `ExitErrHandler` back on the returned command; `cli.Root` keeps the inert handler with no door to remove it.
- `cli.Register(...)` copies the root's `Writer` and `ErrWriter` onto the command it registers, so a stream set on the root command afterwards does not reach it. `Root.SetWriter` and `Root.SetErrorWriter` reach the commands too, whenever they were registered. The engine defaults each command's stream separately, to the process's standard output, so a writer set on the tree alone would leave every command writing past it.
- `cli.DispatchCommand(...)` adds none of what `Register` adds around a command — no banner, no scope close, no exit handling — and is for a caller that runs one command inside a process it owns, such as a scheduler. One engine behaviour travels with its `ctx`: a command dispatched with a context that **descends from another command's action** inherits that command's flag set, so an argument naming a flag only the outer command declares is accepted rather than refused. Dispatch from the process's own context, which is what every caller inside Melody does, and the command is parsed against its own flags alone.
- Registered command execution closes `runtimeInstance.Scope()` after `Run(...)` and may fold that close's failure into the command's result; the container is deliberately left open on either outcome — the recover handler that owns the exit resolves the final record's logger through it and closes it between the record and `os.Exit`. On a panic the finish banner reports `[failed]` before the panic is re-raised unchanged.
