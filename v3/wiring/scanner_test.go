package wiring

import (
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
