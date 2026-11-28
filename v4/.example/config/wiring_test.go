package config

import (
    "os"
    "testing"

    melodywiring "github.com/precision-soft/melody/v4/wiring"
)

const (
    generatedWiringFile = "../generated/wiring_gen.go"
    generatedPackage    = "generated"
    /* the tests run one level below the application root, the directory KernelProjectDirectory reports */
    wiringProjectDirectory = ".."
)

func generateWiring(t *testing.T) (string, *melodywiring.GenerateReport) {
    t.Helper()

    source, report, generateErr := melodywiring.Generate(&melodywiring.GenerateRequest{
        ProjectDirectory: wiringProjectDirectory,
        Config:           NewWiringGenerateConfig(),
        BindSet:          NewWiringBindSet(),
    })
    if nil != generateErr {
        t.Fatalf("expected the wiring to generate, got %v", generateErr)
    }

    return source, report
}

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

func TestWiring_CoversEveryConstructorInTheScannedPackages(t *testing.T) {
    _, report := generateWiring(t)

    if 16 != report.ConstructorCount {
        t.Fatalf("the scan found %d constructors, wanted 16 — add or remove one and update this number with it", report.ConstructorCount)
    }

    if 1 != report.ScopedConstructorCount {
        t.Fatalf("the scan found %d scoped constructors, wanted 1 — the request report trail — add or remove one and update this number with it", report.ScopedConstructorCount)
    }

    if 0 != len(report.Skipped) {
        t.Fatalf("expected no constructor to be skipped, got %v", report.Skipped)
    }
}

func TestWiring_DeclaresNoUnusedBinds(t *testing.T) {
    _, report := generateWiring(t)

    if 0 != len(report.UnusedBinds) {
        t.Fatalf("expected every declared bind to be used, got %v", report.UnusedBinds)
    }
}
