package config

import (
    "testing"
)

/* @info the cli configuration is what a console banner and every suggestion table are built from, and nothing had ever built one: the name is TRIMMED before it is validated, which is the whole reason a name of spaces is refused rather than registered under a spelling no argv ever produces — the lesson the cli session paid for on command names. */
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

/* @info an empty name and a name of nothing but spaces are the same mistake once the trim has run, and both have to be refused: a console registered under "   " answers to a spelling no caller can type. */
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

/* @info a description is optional — a console that describes itself with nothing is a legitimate declaration, and refusing it would make the name the only thing an application may leave out. */
func TestNewCliConfiguration_AnEmptyDescriptionIsAccepted(t *testing.T) {
    cliConfigurationInstance, newErr := newCliConfiguration("melody", "")
    if nil != newErr {
        t.Fatalf("unexpected error: %v", newErr)
    }

    if "" != cliConfigurationInstance.Description() {
        t.Fatalf("expected an empty description to survive, got %q", cliConfigurationInstance.Description())
    }
}
