package application

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type recordingLogger struct {
    warnings []loggingcontract.Context
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *recordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *recordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *recordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.warnings = append(instance.warnings, context)
}

func (instance *recordingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *recordingLogger) Emergency(message string, context loggingcontract.Context) {}

func TestWarnIgnoredProcessEnvironment_WarnsOnKnownParameterName(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    logger := &recordingLogger{}

    warnIgnoredProcessEnvironment(logger, configuration, []string{
        "MELODY_PROCESS_ROLE=worker",
    })

    if 1 != len(logger.warnings) {
        t.Fatalf("expected one warning, got %d", len(logger.warnings))
    }

    if "MELODY_PROCESS_ROLE" != logger.warnings[0]["environmentVariable"] {
        t.Fatalf("unexpected warning context: %+v", logger.warnings[0])
    }

    for key := range logger.warnings[0] {
        if "environmentVariable" != key {
            t.Fatalf("the warning must not carry values, got key: %s", key)
        }
    }
}

func TestWarnIgnoredProcessEnvironment_IgnoresUnknownVariables(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    logger := &recordingLogger{}

    warnIgnoredProcessEnvironment(logger, configuration, []string{
        "PATH=/usr/bin",
        "HOME=/home/user",
        "SOME_RANDOM_VARIABLE=value",
    })

    if 0 != len(logger.warnings) {
        t.Fatalf("expected no warnings, got %d: %+v", len(logger.warnings), logger.warnings)
    }
}

func TestWarnIgnoredProcessEnvironment_SuppressesMirroredValues(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    logger := &recordingLogger{}

    /* a platform that mirrors the resolved .env value into the environment is harmless: same value, no drift possible */
    warnIgnoredProcessEnvironment(logger, configuration, []string{
        "MELODY_PROCESS_ROLE=all",
    })

    if 0 != len(logger.warnings) {
        t.Fatalf("expected the mirrored value to be suppressed, got %d warnings", len(logger.warnings))
    }
}

func TestWarnIgnoredProcessEnvironment_SkipsMalformedEntries(t *testing.T) {
    configuration := newCollisionTestConfiguration(t)
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    logger := &recordingLogger{}

    warnIgnoredProcessEnvironment(logger, configuration, []string{
        "MALFORMED",
        "=value",
    })

    if 0 != len(logger.warnings) {
        t.Fatalf("expected no warnings, got %d", len(logger.warnings))
    }
}

var _ loggingcontract.Logger = (*recordingLogger)(nil)

func TestBootLogger_FallsBackToTheEmergencyLoggerWhenTheContainerHasNone(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    logger := applicationInstance.bootLogger()
    if nil == logger {
        t.Fatalf("expected a logger even when the container carries none")
    }

    /* it must be usable, not merely non-nil: the warnings around the wiring are written through it */
    logger.Warning("probe", loggingcontract.Context{})
}
