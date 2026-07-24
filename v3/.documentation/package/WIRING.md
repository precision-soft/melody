# WIRING

The [`wiring`](../../wiring) package renders container service registrations from the source at build time. It walks the constructors of the packages you declare with `go/parser`, resolves each constructor's dependencies — services from the container by type, scalars from the configuration parameter they are bound to — and writes an ordinary Go file that registers them all. Go cannot enumerate a package's types at run time, and a package no one imports is not in the binary at all, so the scan Symfony performs when it compiles its container is done here at build time; the generated file being ordinary source means an unfilled dependency is a compile error rather than a boot-time surprise.

## Scope

The wiring generator is responsible for:

- declaring which packages to scan and how scalar constructor arguments are filled ([`BindSet`](../../wiring/bind_set.go)),
- discovering every `NewX` constructor under those packages and describing what it returns and needs ([`Scan`](../../wiring/scanner.go)),
- rendering type-correct registration source that a normal build compiles ([`Generate`](../../wiring/generator.go)),
- exposing the generation as the in-application command [`melody:wiring:generate`](../../wiring/generate_command.go).

## Subpackages

None. The package exposes `BindSet`/`PackageBinding`, the `Generate`/`Scan` entry points and the `GenerateCommand`.

## How it works

A constructor is any exported `func NewX(...) (T, error)` or `func NewX(...) T` with a named return type. For each one, every argument that is **not** a scalar is resolved from the container by type (`container.FromResolverByType`); every scalar argument (a string, a number, a bool, a `time.Duration`) is filled from the configuration parameter it is **bound** to.

A bind maps an argument name to a parameter name, declared in one of three scopes, in order of precedence:

1. on the constructor itself, with a `//melody:bind argumentName=parameter.name` directive;
2. on the scanned package, with `PackageBinding.Name(argumentName, parameterName)`;
3. across every scanned package, with `BindSet.Name(argumentName, parameterName)`.

`//melody:ignore` skips a constructor entirely. `//melody:service SomeConstant` additionally registers the service under the name the package already exports (referencing the constant, not copying its value), so the name-based lookups a package exposes keep resolving once its registration moves to the generator.

Generation **fails** when a scalar argument no bind covers, and — when run through the command, which executes inside the booted application — when a bind points at a parameter the running configuration does not declare. A constructor the generator cannot wire (generic, variadic, returning more than a value and an error, returning only an `error`, `any` or a bare scalar) is reported with its location and reason rather than dropped silently, as is every declared bind that matched no argument. `--strict` turns those losses into a failure.

## The command

`melody:wiring:generate` is **not** auto-registered; wire it in the composition root against a `BindSet`, so it checks binds against the parameters the running configuration actually declares:

```go
cli.RegisterCommand(wiring.NewGenerateCommand(config.NewWiringBindSet()))
```

Flags: `--out <path>` (write to a file relative to the project directory; prints to stdout when empty), `--package <name>` (default `config`), `--function <name>` (default `RegisterGeneratedServices`), `--strict`, `--tags <a,b>` (build tags the target binary carries), `--report-vendor` (name the vendor directories the scan stepped over), `--report-excluded` (name the build-excluded files that hold a constructor candidate). The committed generated file is what the application registers, so it is regenerated whenever a scanned constructor changes and kept under version control.

A file the build excludes contributes nothing to the binary the wiring is generated for, so the scan skips it — the unsatisfied half of a tag pair, a foreign `GOOS` suffix, a `//go:build ignore` script. `--tags` tells the scan which tags the binary carries, so a constructor gated on one of them is scanned instead of dropped with its service silently missing; the value is a comma-separated list of plain tag identifiers, not a constraint expression, and anything else is rejected rather than quietly matching no file. The generated source is then specific to that tag set and must be built with the same tags, since it names constructors an untagged build does not have. `--report-excluded` names the excluded files that do hold a constructor candidate, so a service missing from the output traces back to the tag it needs; `--strict` does not fail on them, because the scan cannot know which target the file was written for.

## Usage

Declare the bind set — which packages to scan and how their scalar arguments are filled:

```go
func NewWiringBindSet() *wiring.BindSet {
	bindSet := wiring.NewBindSet()

	bindSet.Package("github.com/acme/app/repository", "repository")

	bindSet.Package("github.com/acme/app/service", "service")

	bindSet.Package("github.com/acme/app/reporting", "reporting").
		Name("catalogTitle", "app.catalog_title").
		Name("maxItemsPerPage", "app.max_items_per_page")

	return bindSet
}
```

Every constructor argument that is a service needs no bind — it is resolved by type. Only the scalars that cannot come from the container are named. Regenerate after adding a constructor:

```
go run . melody:wiring:generate --package generated --function RegisterGeneratedServices --out generated/wiring_gen.go
```

and delegate to the generated function from `RegisterServices`:

```go
func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
	generated.RegisterGeneratedServices(registrar)
}
```

## Footguns & caveats

- The scan is directory-relative to the **project directory** the running application reports (`kernel.project_dir`), the same directory `--out` writes into; the declared directories are plain paths joined onto it.
- The scanned directories must not include the package the generated file is written into, or the generated file would import its own package.
- A constructor whose scalar argument narrows the accessor's widest type (a `uint8`, a `byte`) is range-guarded in the generated provider, so an out-of-range parameter is an error naming the parameter rather than a silent wrap.
- The generated file opens with the `// Code generated ... DO NOT EDIT.` marker Go tooling recognizes; edit the constructors and regenerate, never the generated file.

## Userland API

### Constructors and helpers (`wiring`)

- [`NewBindSet() *BindSet`](../../wiring/bind_set.go)
- [`BindSet.Package(importPath, directory string) *PackageBinding`](../../wiring/bind_set.go)
- [`BindSet.Name(argumentName, parameterName string) *BindSet`](../../wiring/bind_set.go)
- [`PackageBinding.Name(argumentName, parameterName string) *PackageBinding`](../../wiring/bind_set.go)
- [`PackageBinding.Exclude(pattern string) *PackageBinding`](../../wiring/bind_set.go)
- [`NewGenerateCommand(bindSet *BindSet) *GenerateCommand`](../../wiring/generate_command.go)
- [`Generate(request *GenerateRequest) (string, *GenerateReport, error)`](../../wiring/generator.go)
- [`Scan(projectDirectory string, packageBinding *PackageBinding, buildTags []string) (*ScanResult, error)`](../../wiring/scanner.go)

### Directives

- `//melody:bind argumentName=parameter.name` — bind a scalar argument to a parameter, on the constructor.
- `//melody:service SomeExportedConstant` — also register the service under the exported name constant.
- `//melody:ignore` — skip the constructor.
