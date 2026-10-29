package config

import (
    "os"
    "testing"

    melodywiring "github.com/precision-soft/melody/v4/wiring"
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

/* the generated file is what the application registers, so it has to stay what the generator produces; .dev/generate.sh writes it at the dev container's start and before every gate, and without this check a constructor edited since gains an argument and the wiring silently stops matching it */
func TestWiring_GeneratedFileIsUpToDate(t *testing.T) {
    source, _ := generateWiring(t)

    committed, readErr := os.ReadFile(generatedWiringFile)
    if nil != readErr {
        t.Fatalf("could not read the generated wiring, which go generate ./%s writes: %v", generatedPackage, readErr)
    }

    if string(committed) != source {
        t.Fatalf(
            "the generated wiring is out of date; regenerate with: go generate ./%s (or .dev/generate.sh)",
            generatedPackage,
        )
    }
}

/* the two assertions carry different properties and neither implies the other. The skip list is "nothing the scan found was left out"; the count is "the scan finds every constructor it should", the half that notices a package silently dropping out of the bind set, which loses constructors with nothing skipped. The report offers counts and not names, so the count moves whenever a constructor is added or removed, and the number is updated in the same edit that does it. */
func TestWiring_CoversEveryConstructorInTheScannedPackages(t *testing.T) {
    _, report := generateWiring(t)

    if 16 != report.ConstructorCount {
        t.Fatalf("the scan found %d constructors, wanted 16 — add or remove one and update this number with it", report.ConstructorCount)
    }

    /* the scoped half is counted apart: a scoped constructor the generator stops emitting moves neither the number above nor the skip list */
    if 1 != report.ScopedConstructorCount {
        t.Fatalf("the scan found %d scoped constructors, wanted 1 — the request report trail — add or remove one and update this number with it", report.ScopedConstructorCount)
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
