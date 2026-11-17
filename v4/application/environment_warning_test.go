package application

import (
    "maps"
    "testing"

    "github.com/precision-soft/melody/v4/internal"
)

func TestIgnoredProcessEnvironmentBootWarnings_NamesEachKnownParameterNameOnceUnderItsCodeInOrder(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    bootWarnings := ignoredProcessEnvironmentBootWarnings(configuration, []string{
        "MELODY_PROCESS_ROLE=worker",
        "MELODY_ENV=prod",
    })

    if 2 != len(bootWarnings) {
        t.Fatalf("expected one boot warning per name, got %d: %+v", len(bootWarnings), bootWarnings)
    }

    for index, expectedName := range []string{"MELODY_ENV", "MELODY_PROCESS_ROLE"} {
        bootWarning := bootWarnings[index]
        if internal.BootWarningNameProcessEnvironmentVariableIgnored != bootWarning.Name {
            t.Fatalf("expected the code %q, got %q", internal.BootWarningNameProcessEnvironmentVariableIgnored, bootWarning.Name)
        }

        if expectedName != bootWarning.Context["environmentVariable"] {
            t.Fatalf("expected the names in order, %q at %d, got %+v", expectedName, index, bootWarning.Context)
        }

        for key := range bootWarning.Context {
            if "environmentVariable" != key {
                t.Fatalf("the boot warning must not carry values, got key: %s", key)
            }
        }
    }
}

func TestIgnoredProcessEnvironmentBootWarnings_IgnoresUnknownVariables(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    bootWarnings := ignoredProcessEnvironmentBootWarnings(configuration, []string{
        "PATH=/usr/bin",
        "HOME=/home/user",
        "SOME_RANDOM_VARIABLE=value",
    })

    if 0 != len(bootWarnings) {
        t.Fatalf("expected no boot warnings, got %d: %+v", len(bootWarnings), bootWarnings)
    }
}

func TestIgnoredProcessEnvironmentBootWarnings_SuppressesMirroredValues(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    bootWarnings := ignoredProcessEnvironmentBootWarnings(configuration, []string{
        "MELODY_PROCESS_ROLE=all",
    })

    if 0 != len(bootWarnings) {
        t.Fatalf("expected the mirrored value to be suppressed, got %d boot warnings", len(bootWarnings))
    }
}

func TestIgnoredProcessEnvironmentBootWarnings_SkipsMalformedEntries(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    bootWarnings := ignoredProcessEnvironmentBootWarnings(configuration, []string{
        "MALFORMED",
        "=value",
    })

    if 0 != len(bootWarnings) {
        t.Fatalf("expected no boot warnings, got %d", len(bootWarnings))
    }
}

func TestIgnoredProcessEnvironmentBootWarnings_RetiredNamesCarryTheirSuccessor(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)

    key := environmentKeyText()
    key.Retired = []string{"MELODY_X_TXT"}
    if declareErr := configuration.DeclareEnvironmentKey("x", key); nil != declareErr {
        t.Fatalf("unexpected declaration error: %v", declareErr)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    bootWarnings := ignoredProcessEnvironmentBootWarnings(configuration, []string{
        "MELODY_X_TXT=secret",
        "MELODY_PUBLIC_DIR=public",
        "MELODY_RETIRED_BY_NOBODY=value",
    })

    expected := []map[string]any{
        {"environmentVariable": "MELODY_PUBLIC_DIR", "successor": "MELODY_PUBLIC_DIRECTORY"},
        {"environmentVariable": "MELODY_X_TXT", "successor": "MELODY_X_TEXT"},
    }
    if len(expected) != len(bootWarnings) {
        t.Fatalf("expected one boot warning per retired name, got %+v", bootWarnings)
    }

    for index, bootWarning := range bootWarnings {
        if internal.BootWarningNameProcessEnvironmentVariableIgnored != bootWarning.Name || false == maps.Equal(expected[index], map[string]any(bootWarning.Context)) {
            t.Fatalf("expected %v at %d, got %+v", expected[index], index, bootWarning)
        }
    }
}
