package wiring

import (
    "go/parser"
    "go/token"
    "strings"
    "testing"
)

func scanFixture(t *testing.T) *ScanResult {
    t.Helper()

    bindSet := NewBindSet()

    packageBinding := bindSet.Package(fixtureImportPath, fixtureDirectory)

    scanResult, scanErr := Scan(fixtureProjectDir, packageBinding)
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

    _, scanErr := Scan(fixtureProjectDir, bindSet.Package("github.com/acme/missing", "wiring/internal/fixture/missing"))
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
