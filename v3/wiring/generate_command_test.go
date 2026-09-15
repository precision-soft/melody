package wiring

import (
    "bytes"
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v3/cli"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    commandFixtureImportPath       = "example.com/generated/app"
    commandFixtureBrokenImportPath = "example.com/generated/broken"
)

type emptyEnvironmentSource struct {
}

func (instance *emptyEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{}, nil
}

func writeCommandFixtureFile(t *testing.T, projectDirectory string, relativePath string, content string) {
    t.Helper()

    fullPath := filepath.Join(projectDirectory, relativePath)
    if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); nil != mkdirErr {
        t.Fatalf("mkdir: %v", mkdirErr)
    }

    if writeErr := os.WriteFile(fullPath, []byte(content), 0o644); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }
}

func newCommandFixtureProject(t *testing.T) string {
    t.Helper()

    projectDirectory := t.TempDir()

    writeCommandFixtureFile(
        t,
        projectDirectory,
        "app/service.go",
        "package app\n\ntype Thing struct{}\n\nfunc NewThing() *Thing {\n    return &Thing{}\n}\n",
    )

    writeCommandFixtureFile(
        t,
        projectDirectory,
        "app/postgres.go",
        "//go:build with_postgres\n\npackage app\n\ntype Postgres struct{}\n\nfunc NewPostgres() *Postgres {\n    return &Postgres{}\n}\n",
    )

    writeCommandFixtureFile(
        t,
        projectDirectory,
        "app/vendor/dependency/dependency.go",
        "package dependency\n\ntype Vendored struct{}\n\nfunc NewVendored() *Vendored {\n    return &Vendored{}\n}\n",
    )

    writeCommandFixtureFile(
        t,
        projectDirectory,
        "broken/broken.go",
        "package broken\n\ntype Broken struct{}\n\nfunc NewBroken() (*Broken, string, error) {\n    return &Broken{}, \"\", nil\n}\n",
    )

    return projectDirectory
}

func newCommandFixtureRuntime(t *testing.T, projectDirectory string) runtimecontract.Runtime {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(&emptyEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    serviceContainer := container.NewContainer()

    container.MustRegister[configcontract.Configuration](
        serviceContainer,
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
    )

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func runGenerateCommand(
    t *testing.T,
    projectDirectory string,
    bindSet *BindSet,
    arguments ...string,
) (string, error) {
    t.Helper()

    command := NewGenerateCommand(bindSet)
    runtimeInstance := newCommandFixtureRuntime(t, projectDirectory)

    output := &bytes.Buffer{}

    runErr := melodycli.DispatchCommand(
        context.Background(),
        command,
        runtimeInstance,
        append([]string{command.Name()}, arguments...),
        output,
    )

    return output.String(), runErr
}

func appBindSet() *BindSet {
    bindSet := NewBindSet()
    bindSet.Package(commandFixtureImportPath, "app")

    return bindSet
}

func TestGenerateCommand_WithoutTheTagTheGatedConstructorIsAbsentAndItsFileIsNamedExcluded(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    output, runErr := runGenerateCommand(t, projectDirectory, appBindSet(), "--report-excluded")
    if nil != runErr {
        t.Fatalf("expected the generation to succeed, got %v", runErr)
    }

    if false == strings.Contains(output, "NewThing") {
        t.Fatalf("expected the untagged constructor in the generated source, got:\n%s", output)
    }

    if true == strings.Contains(output, "NewPostgres") {
        t.Fatalf("expected the tagged constructor to stay out of the generated source, got:\n%s", output)
    }

    if false == strings.Contains(output, "registered 1 constructors") {
        t.Fatalf("expected the report to name the constructor count, got:\n%s", output)
    }

    if false == strings.Contains(output, "excluded by build constraints") ||
        false == strings.Contains(output, filepath.Join("app", "postgres.go")) {
        t.Fatalf("expected the build-excluded file named on request, got:\n%s", output)
    }
}

func TestGenerateCommand_ThreadsTheTagsFlagIntoTheScan(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    output, runErr := runGenerateCommand(
        t,
        projectDirectory,
        appBindSet(),
        "--tags",
        "with_postgres",
        "--report-excluded",
    )
    if nil != runErr {
        t.Fatalf("expected the generation to succeed, got %v", runErr)
    }

    if false == strings.Contains(output, "NewPostgres") {
        t.Fatalf("expected the tagged constructor in the generated source once its tag is passed, got:\n%s", output)
    }

    if false == strings.Contains(output, "registered 2 constructors") {
        t.Fatalf("expected both constructors registered, got:\n%s", output)
    }

    if true == strings.Contains(output, "excluded by build constraints") {
        t.Fatalf("expected nothing reported as excluded once the tag is passed, got:\n%s", output)
    }
}

func TestGenerateCommand_RejectsAConstraintExpressionInTheTagsFlag(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    _, runErr := runGenerateCommand(t, projectDirectory, appBindSet(), "--tags", "!with_postgres")
    if nil == runErr {
        t.Fatalf("expected a constraint expression to fail the command")
    }

    if false == strings.Contains(runErr.Error(), "plain tag identifier") {
        t.Fatalf("unexpected error %v", runErr)
    }
}

func TestGenerateCommand_StrictFailsOnASkippedConstructor(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    brokenBindSet := NewBindSet()
    brokenBindSet.Package(commandFixtureBrokenImportPath, "broken")

    output, runErr := runGenerateCommand(t, projectDirectory, brokenBindSet)
    if nil != runErr {
        t.Fatalf("expected a skipped constructor to stay non-fatal without strict, got %v", runErr)
    }

    if false == strings.Contains(output, "skipped NewBroken") {
        t.Fatalf("expected the skipped constructor named in the report, got:\n%s", output)
    }

    brokenBindSet = NewBindSet()
    brokenBindSet.Package(commandFixtureBrokenImportPath, "broken")

    _, strictErr := runGenerateCommand(t, projectDirectory, brokenBindSet, "--strict")
    if nil == strictErr {
        t.Fatalf("expected strict to fail on a skipped constructor")
    }

    if false == strings.Contains(strictErr.Error(), "constructors were skipped") {
        t.Fatalf("unexpected strict error %v", strictErr)
    }
}

func TestGenerateCommand_WritesTheGeneratedSourceToTheOutPath(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    output, runErr := runGenerateCommand(
        t,
        projectDirectory,
        appBindSet(),
        "--out",
        filepath.Join("internal", "generated", "wiring_gen.go"),
        "--package",
        "generated",
        "--function",
        "RegisterAppServices",
    )
    if nil != runErr {
        t.Fatalf("expected the generation to succeed, got %v", runErr)
    }

    outputPath := filepath.Join(projectDirectory, "internal", "generated", "wiring_gen.go")

    written, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("expected the generated wiring written to the out path: %v", readErr)
    }

    writtenSource := string(written)

    if false == strings.Contains(writtenSource, generatedFileNote) {
        t.Fatalf("expected the generated file note in the written file, got:\n%s", writtenSource)
    }

    if false == strings.Contains(writtenSource, "package generated") {
        t.Fatalf("expected the package flag honoured in the written file, got:\n%s", writtenSource)
    }

    if false == strings.Contains(writtenSource, "func RegisterAppServices(") {
        t.Fatalf("expected the function flag honoured in the written file, got:\n%s", writtenSource)
    }

    if false == strings.Contains(writtenSource, "NewThing") {
        t.Fatalf("expected the constructor in the written file, got:\n%s", writtenSource)
    }

    if true == strings.Contains(output, generatedFileNote) {
        t.Fatalf("expected the source to go to the file rather than the writer, got:\n%s", output)
    }

    if false == strings.Contains(output, "wiring written to "+outputPath) {
        t.Fatalf("expected the written path reported, got:\n%s", output)
    }
}

func TestGenerateCommand_NamesTheVendorDirectoryOnlyOnRequest(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    quiet, runErr := runGenerateCommand(t, projectDirectory, appBindSet())
    if nil != runErr {
        t.Fatalf("expected the generation to succeed, got %v", runErr)
    }

    if true == strings.Contains(quiet, "skipped vendor directory") {
        t.Fatalf("expected the vendor tree to stay unreported by default, got:\n%s", quiet)
    }

    verbose, runErr := runGenerateCommand(t, projectDirectory, appBindSet(), "--report-vendor")
    if nil != runErr {
        t.Fatalf("expected the generation to succeed, got %v", runErr)
    }

    if false == strings.Contains(verbose, "skipped vendor directory") ||
        false == strings.Contains(verbose, filepath.Join("app", "vendor")) {
        t.Fatalf("expected the vendor tree named on request, got:\n%s", verbose)
    }
}

func TestSplitBuildTags_RejectsAConstraintExpression(t *testing.T) {
    for _, tags := range []string{"!postgres", "postgres,!mysql", "postgres mysql", "post-gres", "(postgres)"} {
        buildTags, splitErr := splitBuildTags(tags)

        if nil == splitErr {
            t.Fatalf("expected %q to be rejected, got tags %v", tags, buildTags)
        }

        if nil != buildTags {
            t.Fatalf("expected %q to yield no tags, got %v", tags, buildTags)
        }
    }
}

func TestSplitBuildTags_AcceptsPlainIdentifiers(t *testing.T) {
    buildTags, splitErr := splitBuildTags(" with_postgres , go1.22,, Integration2 ")
    if nil != splitErr {
        t.Fatalf("expected plain identifiers to be accepted: %v", splitErr)
    }

    expected := []string{"with_postgres", "go1.22", "Integration2"}
    if len(expected) != len(buildTags) {
        t.Fatalf("expected %v, got %v", expected, buildTags)
    }

    for index, tag := range expected {
        if tag != buildTags[index] {
            t.Fatalf("expected %v, got %v", expected, buildTags)
        }
    }
}

func TestSplitBuildTags_EmptyInputYieldsNoTags(t *testing.T) {
    buildTags, splitErr := splitBuildTags("")
    if nil != splitErr {
        t.Fatalf("expected an empty tag list to be accepted: %v", splitErr)
    }

    if nil != buildTags {
        t.Fatalf("expected an empty tag list to yield no tags, got %v", buildTags)
    }
}

func TestGenerateCommand_StrictCarriesEveryViolationInOneRefusal(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    bindSet := NewBindSet()
    bindSet.Name("ghostArgument", "app.ghost")
    bindSet.Package(commandFixtureBrokenImportPath, "broken").Exclude("*Ghost")

    _, strictErr := runGenerateCommand(t, projectDirectory, bindSet, "--strict")
    if nil == strictErr {
        t.Fatalf("expected strict to fail")
    }

    var refusal *exception.Error
    if false == errors.As(strictErr, &refusal) {
        t.Fatalf("expected an exception error, got %T", strictErr)
    }

    refusalContext := refusal.Context()

    binds, _ := refusalContext["binds"].(string)
    if false == strings.Contains(binds, "ghostArgument") {
        t.Fatalf("expected the unused bind in the refusal, got %v", refusalContext)
    }

    excludes, _ := refusalContext["excludes"].(string)
    if false == strings.Contains(excludes, "*Ghost") {
        t.Fatalf("expected the unused exclude in the refusal, got %v", refusalContext)
    }

    skipped, _ := refusalContext["constructors"].(string)
    if false == strings.Contains(skipped, "NewBroken") {
        t.Fatalf("expected the skipped constructor in the refusal, got %v", refusalContext)
    }
}

func TestGenerateCommand_RefusesAnOutPathInsideAScannedDirectory(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    _, runErr := runGenerateCommand(
        t,
        projectDirectory,
        appBindSet(),
        "--out",
        filepath.Join("app", "wiring_gen.go"),
    )
    if nil == runErr {
        t.Fatalf("expected the out path inside the scanned directory to be refused")
    }

    if false == strings.Contains(runErr.Error(), "the output path lies inside a scanned package directory") {
        t.Fatalf("unexpected error: %v", runErr)
    }
}

func TestGenerateCommand_RefusesToOverwriteAFileWithoutTheGeneratedMarker(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    writeCommandFixtureFile(t, projectDirectory, "config/module.go", "package config\n\nfunc Wire() {}\n")

    _, runErr := runGenerateCommand(
        t,
        projectDirectory,
        appBindSet(),
        "--out",
        filepath.Join("config", "module.go"),
    )
    if nil == runErr {
        t.Fatalf("expected the foreign file to be protected")
    }

    if false == strings.Contains(runErr.Error(), "is not a generated wiring file") {
        t.Fatalf("unexpected error: %v", runErr)
    }

    preserved, readErr := os.ReadFile(filepath.Join(projectDirectory, "config", "module.go"))
    if nil != readErr || false == strings.Contains(string(preserved), "func Wire()") {
        t.Fatalf("expected the foreign file preserved, got %q (%v)", string(preserved), readErr)
    }
}

func TestGenerateCommand_ReplacesAPreviousGeneratedFile(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    outArgument := filepath.Join("internal", "generated", "wiring_gen.go")

    for run := 0; run < 2; run++ {
        _, runErr := runGenerateCommand(t, projectDirectory, appBindSet(), "--out", outArgument)
        if nil != runErr {
            t.Fatalf("run %d: %v", run, runErr)
        }
    }

    written, readErr := os.ReadFile(filepath.Join(projectDirectory, outArgument))
    if nil != readErr || false == strings.Contains(string(written), "NewThing") {
        t.Fatalf("expected the regenerated file, got %v", readErr)
    }
}

func TestGenerateCommand_AtomicWriteLeavesTheModeAndNoResidue(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    outArgument := filepath.Join("internal", "generated", "wiring_gen.go")

    _, runErr := runGenerateCommand(t, projectDirectory, appBindSet(), "--out", outArgument)
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    outputPath := filepath.Join(projectDirectory, outArgument)

    fileInfo, statErr := os.Stat(outputPath)
    if nil != statErr {
        t.Fatalf("stat: %v", statErr)
    }

    if 0o644 != fileInfo.Mode().Perm() {
        t.Fatalf("expected mode 0644, got %v", fileInfo.Mode().Perm())
    }

    entries, readDirErr := os.ReadDir(filepath.Dir(outputPath))
    if nil != readDirErr {
        t.Fatalf("read dir: %v", readDirErr)
    }

    for _, entry := range entries {
        if true == strings.HasSuffix(entry.Name(), ".tmp") {
            t.Fatalf("expected no temp residue, found %s", entry.Name())
        }
    }
}

func TestGenerateCommand_ReportsAnUnusedExcludeOnTheWriter(t *testing.T) {
    projectDirectory := newCommandFixtureProject(t)

    bindSet := NewBindSet()
    bindSet.Package(commandFixtureImportPath, "app").Exclude("*Ghost")

    output, runErr := runGenerateCommand(t, projectDirectory, bindSet)
    if nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    if false == strings.Contains(output, "exclude "+commandFixtureImportPath+".*Ghost matched no constructor") {
        t.Fatalf("expected the unused exclude named on the writer, got:\n%s", output)
    }
}
