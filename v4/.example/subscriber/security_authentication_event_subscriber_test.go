package subscriber

import (
    "context"
    "errors"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

type levelRecordingLogger struct {
    melodyloggingcontract.Logger
    levels   []string
    contexts []melodyloggingcontract.Context
}

func (instance *levelRecordingLogger) Info(message string, context melodyloggingcontract.Context) {
    instance.levels = append(instance.levels, "info")
    instance.contexts = append(instance.contexts, context)
}

func (instance *levelRecordingLogger) Warning(message string, context melodyloggingcontract.Context) {
    instance.levels = append(instance.levels, "warning")
    instance.contexts = append(instance.contexts, context)
}

func TestSecurityAuthenticationEventSubscriber_JournalsAFailureAsAWarningAtTheEventsInstant(t *testing.T) {
    logger := &levelRecordingLogger{Logger: melodylogging.NewNopLogger()}
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    instant := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
    listener := NewSecurityAuthenticationEventSubscriber().onLoginFailure()
    if listenErr := listener(runtimeInstance, melodyevent.NewEvent(melodysecuritycontract.EventSecurityLoginFailure, melodysecurity.NewLoginFailureEvent(nil, errors.New("invalid credentials")), instant)); nil != listenErr {
        t.Fatalf("listener: %v", listenErr)
    }

    if 1 != len(logger.levels) || "warning" != logger.levels[0] || "2026-09-06T10:00:00Z" != logger.contexts[0]["occurredAt"] {
        t.Fatalf("expected one warning at the event's instant, got %v %v", logger.levels, logger.contexts)
    }
}

func TestSecurityAuthenticationEventSubscriber_JournalsASuccessAtInfoAtTheEventsInstant(t *testing.T) {
    logger := &levelRecordingLogger{Logger: melodylogging.NewNopLogger()}
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    instant := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
    listener := NewSecurityAuthenticationEventSubscriber().onLoginSuccess()
    successEvent := melodysecurity.NewLoginSuccessEvent(nil, melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"}, nil))
    if listenErr := listener(runtimeInstance, melodyevent.NewEvent(melodysecuritycontract.EventSecurityLoginSuccess, successEvent, instant)); nil != listenErr {
        t.Fatalf("listener: %v", listenErr)
    }

    if 1 != len(logger.levels) || "info" != logger.levels[0] || "2026-09-06T10:00:00Z" != logger.contexts[0]["occurredAt"] || "user-2" != logger.contexts[0]["userIdentifier"] {
        t.Fatalf("expected one info record at the event's instant, got %v %v", logger.levels, logger.contexts)
    }
}
