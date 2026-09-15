package config

import (
    "os"
    "testing"
)

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

func TestWiring_CoversEveryConstructorInTheScannedPackages(t *testing.T) {
    _, report := generateWiring(t)

    if 15 != report.ConstructorCount {
        t.Fatalf("the scan found %d constructors, wanted 15 — add or remove one and update this number with it", report.ConstructorCount)
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
