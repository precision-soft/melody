package wiring

import (
    "go/parser"
    "go/token"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func scanFixture(t *testing.T) *ScanResult {
    t.Helper()

    bindSet := NewBindSet()

    packageBinding := bindSet.Package(fixtureImportPath, fixtureDirectory)

    scanResult, scanErr := Scan(fixtureProjectDir, packageBinding, nil)
    if nil != scanErr {
        t.Fatalf("expected the fixture to scan, got %v", scanErr)
    }

    return scanResult
}

func constructorByName(scanResult *ScanResult, name string) *Constructor {
    for _, constructor := range scanResult.Constructors {
        if name == constructor.Name {
            return constructor
        }
    }

    return nil
}

func TestScan_ClassifiesArgumentsAsScalarsOrServices(t *testing.T) {
    constructor := constructorByName(scanFixture(t), "NewUserService")
    if nil == constructor {
        t.Fatalf("expected the constructor to be found")
    }

    expectations := map[string]bool{
        "repository": false,
        "logger":     false,
        "sessionTtl": true,
        "stageEnv":   true,
    }

    if len(expectations) != len(constructor.Arguments) {
        t.Fatalf("expected %d arguments, got %d", len(expectations), len(constructor.Arguments))
    }

    for _, argument := range constructor.Arguments {
        expected, exists := expectations[argument.Name]
        if false == exists {
            t.Fatalf("unexpected argument %q", argument.Name)
        }

        if expected != argument.IsScalar {
            t.Fatalf("expected %q scalar classification to be %t", argument.Name, expected)
        }
    }
}

/* @info a time.Duration is a named type from another package but behaves as a scalar for wiring: it can only come from configuration, never from the container */
func TestScan_TreatsDurationAsAScalar(t *testing.T) {
    constructor := constructorByName(scanFixture(t), "NewUserService")

    for _, argument := range constructor.Arguments {
        if "sessionTtl" != argument.Name {
            continue
        }

        if false == argument.IsScalar {
            t.Fatalf("expected a duration argument to be treated as a scalar")
        }

        if "time.Duration" != argument.Type.Expression {
            t.Fatalf("unexpected type expression %q", argument.Type.Expression)
        }

        return
    }

    t.Fatalf("expected the duration argument to be found")
}

func TestScan_RecordsWhetherTheConstructorReturnsAnError(t *testing.T) {
    scanResult := scanFixture(t)

    if false == constructorByName(scanResult, "NewUserService").ReturnsError {
        t.Fatalf("expected the error return to be recorded")
    }

    if true == constructorByName(scanResult, "NewInvoiceService").ReturnsError {
        t.Fatalf("expected the single-value return to be recorded")
    }
}

/* @info a nested directory is its own package, so its import path has to be derived rather than inherited from the declared root */
func TestScan_DerivesTheImportPathOfANestedPackage(t *testing.T) {
    constructor := constructorByName(scanFixture(t), "NewUserService")

    for _, argument := range constructor.Arguments {
        if "logger" != argument.Name {
            continue
        }

        if fixtureImportPath+"/contract" != argument.Type.ImportPath {
            t.Fatalf("unexpected import path %q", argument.Type.ImportPath)
        }

        return
    }

    t.Fatalf("expected the logger argument to be found")
}

func TestScan_ReadsTheBindDirective(t *testing.T) {
    constructor := constructorByName(scanFixture(t), "NewReportingService")
    if nil == constructor {
        t.Fatalf("expected the constructor to be found")
    }

    if "fixture.reporting_url" != constructor.DirectiveBinds["reportingUrl"] {
        t.Fatalf("unexpected directive binds %v", constructor.DirectiveBinds)
    }
}

func TestScan_SkipsTheIgnoreDirective(t *testing.T) {
    scanResult := scanFixture(t)

    if nil != constructorByName(scanResult, "NewExcludedByDirective") {
        t.Fatalf("expected the ignored constructor to be absent")
    }

    for _, skipped := range scanResult.Skipped {
        if "NewExcludedByDirective" == skipped.Name {
            t.Fatalf("expected the ignored constructor to be absent from the skip report as well")
        }
    }
}

/* @info a shape the generator cannot wire has to be named, otherwise the run silently covers less than it appears to */
func TestScan_ReportsUnwireableShapesWithTheirLocation(t *testing.T) {
    scanResult := scanFixture(t)

    for _, name := range []string{"NewGenericHolder", "NewVariadicService"} {
        found := false

        for _, skipped := range scanResult.Skipped {
            if name != skipped.Name {
                continue
            }

            found = true

            if "" == skipped.File || 0 == skipped.Line {
                t.Fatalf("expected %s to be reported with a location", name)
            }

            if "" == skipped.Reason {
                t.Fatalf("expected %s to be reported with a reason", name)
            }
        }

        if false == found {
            t.Fatalf("expected %s to be reported as skipped", name)
        }
    }
}

func TestIsExcluded_MatchesTheReturnedTypeName(t *testing.T) {
    cases := []struct {
        typeExpression string
        pattern        string
        expected       bool
    }{
        {"*domain.UserFixture", "*Fixture", true},
        {"*domain.UserService", "*Fixture", false},
        {"domain.Logger", "Logger", true},
        {"*domain.UserService", "User*", true},
    }

    for _, testCase := range cases {
        if testCase.expected != isExcluded(testCase.typeExpression, []string{testCase.pattern}) {
            t.Fatalf("unexpected exclusion of %q by %q", testCase.typeExpression, testCase.pattern)
        }
    }
}

func TestScan_ReportsAMissingDirectory(t *testing.T) {
    bindSet := NewBindSet()

    _, scanErr := Scan(fixtureProjectDir, bindSet.Package("github.com/acme/missing", "wiring/internal/fixture/missing"), nil)
    if nil == scanErr {
        t.Fatalf("expected a missing directory to be reported")
    }

    if false == strings.Contains(scanErr.Error(), "could not scan the package directory") {
        t.Fatalf("unexpected error: %v", scanErr)
    }
}

/* @info a plain prefix match would also claim a longer word — //melody:serviceFoo — and read a name out of what is not the directive at all */
func TestDirectiveRemainder_MatchesTheDirectiveExactly(t *testing.T) {
    cases := []struct {
        text      string
        directive string
        remainder string
        matches   bool
    }{
        {"//melody:service ServiceName", serviceDirective, "ServiceName", true},
        {"//melody:service", serviceDirective, "", true},
        {"//melody:serviceFoo", serviceDirective, "", false},
        {"//melody:bind url=app.url", bindDirective, "url=app.url", true},
        {"//melody:bindx=y", bindDirective, "", false},
    }

    for _, currentCase := range cases {
        remainder, matches := directiveRemainder(currentCase.text, currentCase.directive)

        if currentCase.matches != matches || currentCase.remainder != remainder {
            t.Fatalf(
                "unexpected directive match for %q: got (%q, %v), want (%q, %v)",
                currentCase.text,
                remainder,
                matches,
                currentCase.remainder,
                currentCase.matches,
            )
        }
    }
}

func TestPackageNameCandidates_CoverTheConventionalShapes(t *testing.T) {
    cases := []struct {
        importPath string
        candidates []string
    }{
        {"github.com/precision-soft/melody/v3", []string{"melody"}},
        {"gopkg.in/yaml.v3", []string{"yaml"}},
        {"github.com/redis/go-redis", []string{"redis"}},
        {"example.com/plain", []string{}},
    }

    for _, currentCase := range cases {
        candidates := packageNameCandidates(currentCase.importPath)

        if len(currentCase.candidates) != len(candidates) {
            t.Fatalf("unexpected candidates for %q: got %v, want %v", currentCase.importPath, candidates, currentCase.candidates)
        }

        for index, candidate := range candidates {
            if currentCase.candidates[index] != candidate {
                t.Fatalf("unexpected candidates for %q: got %v, want %v", currentCase.importPath, candidates, currentCase.candidates)
            }
        }
    }
}

/* @info the qualifier a file uses is the package name, which the last path segment does not always spell; without the fallbacks a constructor depending on such a package is skipped */
func TestCollectImports_ResolvesAQualifierThePathBaseDoesNotSpell(t *testing.T) {
    source := `package sample

import (
    "gopkg.in/yaml.v3"
    "github.com/precision-soft/melody/v3"
    redis "github.com/other/custom-alias"
)
`

    fileNode, parseErr := parser.ParseFile(token.NewFileSet(), "sample.go", source, parser.ParseComments)
    if nil != parseErr {
        t.Fatalf("could not parse the sample source: %v", parseErr)
    }

    fileImports := collectImports(fileNode)

    if "gopkg.in/yaml.v3" != fileImports["yaml"] {
        t.Fatalf("expected the yaml qualifier to resolve, got %q", fileImports["yaml"])
    }

    if "github.com/precision-soft/melody/v3" != fileImports["melody"] {
        t.Fatalf("expected the melody qualifier to resolve, got %q", fileImports["melody"])
    }

    /* an explicit alias always wins over a guessed fallback of another import */
    if "github.com/other/custom-alias" != fileImports["redis"] {
        t.Fatalf("expected the explicit alias to win, got %q", fileImports["redis"])
    }
}

/* @info the go tool skips a vendor tree in its package walks and refuses to import a main package, so neither may contribute constructors to the generated wiring */
func TestScan_SkipsVendorDirectoriesAndMainPackages(t *testing.T) {
    projectDirectory := t.TempDir()

    writeScanFile := func(relativePath string, content string) {
        t.Helper()

        fullPath := filepath.Join(projectDirectory, relativePath)
        if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); nil != mkdirErr {
            t.Fatalf("mkdir: %v", mkdirErr)
        }

        if writeErr := os.WriteFile(fullPath, []byte(content), 0o644); nil != writeErr {
            t.Fatalf("write: %v", writeErr)
        }
    }

    writeScanFile("app/service.go", "package app\n\ntype Thing struct{}\n\nfunc NewThing() *Thing {\n    return &Thing{}\n}\n")
    writeScanFile("app/vendor/dep/dep.go", "package dep\n\ntype Vendored struct{}\n\nfunc NewVendored() *Vendored {\n    return &Vendored{}\n}\n")
    writeScanFile("app/cmd/main.go", "package main\n\ntype Program struct{}\n\nfunc NewProgram() *Program {\n    return &Program{}\n}\n\n//melody:ignore\nfunc NewAcknowledged() *Program {\n    return &Program{}\n}\n\nfunc main() {}\n")

    bindSet := NewBindSet()
    binding := bindSet.Package("example.com/proj/app", "app")

    scanResult, scanErr := Scan(projectDirectory, binding, nil)
    if nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if nil == constructorByName(scanResult, "NewThing") {
        t.Fatalf("expected the regular constructor to be found")
    }

    if nil != constructorByName(scanResult, "NewVendored") {
        t.Fatalf("expected the vendored constructor to be skipped")
    }

    if nil != constructorByName(scanResult, "NewProgram") {
        t.Fatalf("expected the main-package constructor to be skipped")
    }

    /* the main-package loss is visible: strict fails on the reported entry unless //melody:ignore acknowledges it */
    var mainSkip *SkippedConstructor
    for _, skipped := range scanResult.Skipped {
        if "NewAcknowledged" == skipped.Name {
            t.Fatalf("expected the ignore directive to acknowledge the main-package constructor, got %+v", skipped)
        }

        if "NewProgram" == skipped.Name {
            mainSkip = skipped
        }
    }

    if nil == mainSkip || false == strings.Contains(mainSkip.Reason, "main package") {
        t.Fatalf("expected the main-package constructor reported as skipped with its reason, got %+v", scanResult.Skipped)
    }

    /* the vendor tree is recorded separately: it cannot contribute services, so strict does not fail on it, but the report can name it on request */
    if 1 != len(scanResult.SkippedVendorDirectories) || false == strings.HasSuffix(scanResult.SkippedVendorDirectories[0], filepath.Join("app", "vendor")) {
        t.Fatalf("expected the vendor directory recorded, got %v", scanResult.SkippedVendorDirectories)
    }
}

/* @info a constructor gated on a build tag the binary carries is dropped by the default build; the scan must be told the tag through --tags to include it, and name it as excluded otherwise so a missing service is traceable */
func TestScan_BuildTaggedConstructorIsExcludedUntilTagIsPassed(t *testing.T) {
    projectDirectory := t.TempDir()

    writeScanFile := func(relativePath string, content string) {
        t.Helper()

        fullPath := filepath.Join(projectDirectory, relativePath)
        if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); nil != mkdirErr {
            t.Fatalf("mkdir: %v", mkdirErr)
        }

        if writeErr := os.WriteFile(fullPath, []byte(content), 0o644); nil != writeErr {
            t.Fatalf("write: %v", writeErr)
        }
    }

    writeScanFile("app/service.go", "package app\n\ntype Thing struct{}\n\nfunc NewThing() *Thing {\n    return &Thing{}\n}\n")
    writeScanFile("app/postgres.go", "//go:build with_postgres\n\npackage app\n\ntype Postgres struct{}\n\nfunc NewPostgres() *Postgres {\n    return &Postgres{}\n}\n")

    bindSet := NewBindSet()
    binding := bindSet.Package("example.com/proj/app", "app")

    withoutTag, scanErr := Scan(projectDirectory, binding, nil)
    if nil != scanErr {
        t.Fatalf("scan without tag: %v", scanErr)
    }

    if nil != constructorByName(withoutTag, "NewPostgres") {
        t.Fatalf("expected the tagged constructor to be excluded without the build tag")
    }

    if 1 != len(withoutTag.ExcludedFiles) || false == strings.HasSuffix(withoutTag.ExcludedFiles[0], filepath.Join("app", "postgres.go")) {
        t.Fatalf("expected the tagged file named as excluded, got %v", withoutTag.ExcludedFiles)
    }

    withTag, scanErr := Scan(projectDirectory, binding, []string{"with_postgres"})
    if nil != scanErr {
        t.Fatalf("scan with tag: %v", scanErr)
    }

    if nil == constructorByName(withTag, "NewPostgres") {
        t.Fatalf("expected the tagged constructor to be scanned once its build tag is passed")
    }

    if 0 != len(withTag.ExcludedFiles) {
        t.Fatalf("expected no excluded files once the tag is passed, got %v", withTag.ExcludedFiles)
    }
}
