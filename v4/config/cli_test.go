package config

import (
    "testing"
)

func TestNewCliConfiguration_TrimsBeforeItValidates(t *testing.T) {
    cliConfigurationInstance, newErr := newCliConfiguration("  melody  ", "  the console  ")
    if nil != newErr {
        t.Fatalf("unexpected error: %v", newErr)
    }

    if "melody" != cliConfigurationInstance.Name() {
        t.Fatalf("expected the name to be trimmed, got %q", cliConfigurationInstance.Name())
    }

    if "the console" != cliConfigurationInstance.Description() {
        t.Fatalf("expected the description to be trimmed, got %q", cliConfigurationInstance.Description())
    }
}

func TestNewCliConfiguration_RefusesANameThatTrimsAway(t *testing.T) {
    for _, name := range []string{"", "   ", "\t\n"} {
        _, newErr := newCliConfiguration(name, "the console")
        if nil == newErr {
            t.Fatalf("expected the name %q to be refused", name)
        }

        if "cli name may not be empty" != newErr.Error() {
            t.Fatalf("unexpected refusal message: %q", newErr.Error())
        }
    }
}

func TestNewCliConfiguration_AnEmptyDescriptionIsAccepted(t *testing.T) {
    cliConfigurationInstance, newErr := newCliConfiguration("melody", "")
    if nil != newErr {
        t.Fatalf("unexpected error: %v", newErr)
    }

    if "" != cliConfigurationInstance.Description() {
        t.Fatalf("expected an empty description to survive, got %q", cliConfigurationInstance.Description())
    }
}
