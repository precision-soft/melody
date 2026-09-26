package journal

import (
    "bytes"
    "context"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func TestLoggerOr_AnswersTheRuntimesLoggerAndTheFallbackWithoutOne(t *testing.T) {
    fallbackJournal := &bytes.Buffer{}
    fallback := melodylogging.NewJsonLogger(fallbackJournal, melodyloggingcontract.LevelDebug)

    if answered := LoggerOr(nil, fallback); fallback != answered {
        t.Errorf("no runtime answered %v, wanted the fallback", answered)
    }

    bareContainer := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = bareContainer.Close() })
    if answered := LoggerOr(melodyruntime.New(context.Background(), bareContainer.NewScope(), bareContainer), fallback); fallback != answered {
        t.Errorf("a runtime without a logger answered %v, wanted the fallback", answered)
    }

    journal := &bytes.Buffer{}
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
    })

    LoggerOr(melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance), fallback).Warning("zz journal door", nil)

    if 0 == journal.Len() || 0 != fallbackJournal.Len() {
        t.Errorf("a runtime with a logger wrote %q to it and %q to the fallback, wanted the record on the runtime's alone", journal.String(), fallbackJournal.String())
    }
}
