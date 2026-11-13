package config

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

func TestNewCliConfiguration_TrimsBeforeItValidates(t *testing.T) {
    cliConfigurationInstance, newErr := newCliConfiguration("  melody  ", "  the console  ", nil, false)
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
        _, newErr := newCliConfiguration(name, "the console", nil, false)
        if nil == newErr {
            t.Fatalf("expected the name %q to be refused", name)
        }

        if "cli name may not be empty" != newErr.Error() {
            t.Fatalf("unexpected refusal message: %q", newErr.Error())
        }
    }
}

func TestNewCliConfiguration_AnEmptyDescriptionIsAccepted(t *testing.T) {
    cliConfigurationInstance, newErr := newCliConfiguration("melody", "", nil, false)
    if nil != newErr {
        t.Fatalf("unexpected error: %v", newErr)
    }

    if "" != cliConfigurationInstance.Description() {
        t.Fatalf("expected an empty description to survive, got %q", cliConfigurationInstance.Description())
    }
}

func TestConfigurationCliBootWarningKeysAreReadAndDefaultToNothing(t *testing.T) {
    defaulted := mustConfigurationFromValues(t, map[string]string{}).Cli()
    if 0 != len(defaulted.BootWarningsIgnored()) || true == defaulted.BootWarningsDisabled() {
        t.Fatalf("expected no ignored code and the journal enabled, got %v %v", defaulted.BootWarningsIgnored(), defaulted.BootWarningsDisabled())
    }

    ignored := mustConfigurationFromValues(t, map[string]string{CliBootWarningsIgnoredKey: " " + internal.BootWarningNameFirewallDoorReachUnknown + " "}).Cli()
    if 1 != len(ignored.BootWarningsIgnored()) || internal.BootWarningNameFirewallDoorReachUnknown != ignored.BootWarningsIgnored()[0] {
        t.Fatalf("expected the trimmed code, got %v", ignored.BootWarningsIgnored())
    }

    ignored.BootWarningsIgnored()[0] = "changed"
    if internal.BootWarningNameFirewallDoorReachUnknown != ignored.BootWarningsIgnored()[0] {
        t.Fatalf("writing into the returned list reached the configuration")
    }

    if false == mustConfigurationFromValues(t, map[string]string{CliBootWarningsDisabledKey: "true"}).Cli().BootWarningsDisabled() {
        t.Fatalf("expected the journal disabled")
    }
}

func TestConfigurationCliBootWarningKeysRefuseWhatSilencesNothing(t *testing.T) {
    for _, testCase := range []struct {
        expected string
        values   map[string]string
    }{
        {"cli boot warning code may not be empty", map[string]string{CliBootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown + ","}},
        {"cli boot warning code is listed twice", map[string]string{CliBootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown + "," + internal.BootWarningNameFirewallDoorReachUnknown}},
        {"cli boot warnings are both disabled and ignored by code: a disabled journal writes no boot warning, so the ignored codes are read by nothing; keep one of the two keys", map[string]string{CliBootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown, CliBootWarningsDisabledKey: "true"}},
        {"invalid environment value", map[string]string{CliBootWarningsDisabledKey: "maybe"}},
    } {
        expected := testCase.expected
        values := testCase.values

        _, err := newConfigurationFromValues(values)

        found := false
        for current := err; nil != current; current = errors.Unwrap(current) {
            if refusal, isRefusal := current.(*exception.Error); true == isRefusal && expected == refusal.Message() {
                found = true
            }
        }

        if false == found {
            t.Fatalf("expected %q for %v, got %v", expected, values, err)
        }
    }

    /* whether a code names a warning a cli process writes is judged at boot, once the modules declared theirs */
    for _, code := range []string{"aws.s3.plaintextEndpoint", internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage} {
        if _, err := newConfigurationFromValues(map[string]string{CliBootWarningsIgnoredKey: code}); nil != err {
            t.Fatalf("expected %q admitted when the configuration is built, got %v", code, err)
        }
    }

    if _, err := newConfigurationFromValues(map[string]string{CliBootWarningsDisabledKey: "false", CliBootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown}); nil != err {
        t.Fatalf("expected a code beside an explicit false admitted, got %v", err)
    }
}
