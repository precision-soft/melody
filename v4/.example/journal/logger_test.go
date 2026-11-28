package journal

import (
    "bytes"
    "context"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestLoggerOr_AnswersTheRuntimesLoggerAndTheFallbackWithoutOne(t *testing.T) {
    fallbackJournal := &bytes.Buffer{}
    fallback := melodylogging.NewJsonLogger(fallbackJournal, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug})

    if answered := LoggerOr(nil, fallback); fallback != answered {
        t.Errorf("no runtime answered %v, wanted the fallback", answered)
    }

    bareContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = bareContainer.CloseWithContext(context.Background()) })
    if answered := LoggerOr(melodyruntime.NewRun(context.Background(), bareContainer.NewScope()), fallback); fallback != answered {
        t.Errorf("a runtime without a logger answered %v, wanted the fallback", answered)
    }

    journal := &bytes.Buffer{}
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewJsonLogger(journal, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug}), nil
    }, melodycontainer.RegisterOptions{})

    LoggerOr(melodyruntime.NewRun(context.Background(), containerInstance.NewScope()), fallback).Warning("zz journal door", nil)

    if 0 == journal.Len() || 0 != fallbackJournal.Len() {
        t.Errorf("a runtime with a logger wrote %q to it and %q to the fallback, wanted the record on the runtime's alone", journal.String(), fallbackJournal.String())
    }
}
