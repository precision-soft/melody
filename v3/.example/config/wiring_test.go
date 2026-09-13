package config

import (
    "os"
    "testing"

    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

const (
    generatedWiringFile = "../generated/wiring_gen.go"
    generatedPackage    = "generated"
    generatedFunction   = "RegisterGeneratedServices"
    /* the tests run from the config package, one level below the application root, so this is the same project directory the running application reports through KernelProjectDir */
    wiringProjectDir = ".."
)

func generateWiring(t *testing.T) (string, *melodywiring.GenerateReport) {
    t.Helper()

    source, report, generateErr := melodywiring.Generate(&melodywiring.GenerateRequest{
        ProjectDirectory: wiringProjectDir,
        PackageName:      generatedPackage,
        FunctionName:     generatedFunction,
        BindSet:          NewWiringBindSet(),
    })
    if nil != generateErr {
        t.Fatalf("expected the wiring to generate, got %v", generateErr)
    }

    return source, report
}

/* the committed file is what the application registers, so it has to stay what the generator produces; without this check a constructor gains an argument and the wiring silently keeps building the old one */
func TestWiring_GeneratedFileIsUpToDate(t *testing.T) {
    source, _ := generateWiring(t)

    committed, readErr := os.ReadFile(generatedWiringFile)
    if nil != readErr {
        t.Fatalf("could not read the generated wiring: %v", readErr)
    }

    if string(committed) != source {
        t.Fatalf(
            "the generated wiring is out of date; regenerate with: go run . melody:wiring:generate --package %s --function %s --out generated/wiring_gen.go",
            generatedPackage,
            generatedFunction,
        )
    }
}

/* the two assertions carry different properties and neither implies the other. The skip list is "nothing the
   scan found was left out"; the count is "the scan still finds what it used to", which is the half that
   notices a package silently dropping out of the bind set — that loses constructors with nothing skipped.
   The report offers counts and not names, so the count is as specific as this can be: it moves whenever a
   constructor is added or removed, deliberately, and the number is updated in the same edit that does it. */
func TestWiring_CoversEveryConstructorInTheScannedPackages(t *testing.T) {
    _, report := generateWiring(t)

    if 15 != report.ConstructorCount {
        t.Fatalf("the scan found %d constructors, wanted 15 — add or remove one and update this number with it", report.ConstructorCount)
    }

    if 0 != len(report.Skipped) {
        t.Fatalf("expected no constructor to be skipped, got %v", report.Skipped)
    }
}

/* a bind that matches nothing is the misspelling this reporting exists to catch, and it must stay empty for the example */
func TestWiring_DeclaresNoUnusedBinds(t *testing.T) {
    _, report := generateWiring(t)

    if 0 != len(report.UnusedBinds) {
        t.Fatalf("expected every declared bind to be used, got %v", report.UnusedBinds)
    }
}
