package output

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func newRendererTestEnvelope() Envelope {
    return NewEnvelope(
        NewMeta(
            "debug:container",
            []string{"missing.service.name"},
            DefaultOption(),
            time.Now(),
            time.Duration(0),
            Version{},
        ),
    )
}

func TestRender_ReturnsAnExitErrorWhenTheEnvelopeCarriesAnError(t *testing.T) {
    buffer := &bytes.Buffer{}

    envelope := newRendererTestEnvelope()
    envelope.SetError(
        "debug.notFound",
        "service not found",
        map[string]any{
            "serviceName": "missing.service.name",
        },
        nil,
    )

    renderErr := Render(buffer, envelope, Option{Format: FormatJson})
    if nil == renderErr {
        t.Fatalf("expected an error for an envelope carrying an error")
    }

    var exitError *exception.ExitError
    if false == errors.As(renderErr, &exitError) {
        t.Fatalf("expected an exit error, got %T", renderErr)
    }
    if 0 == exitError.ExitCode() {
        t.Fatalf("expected a non-zero exit code")
    }

    if false == strings.Contains(buffer.String(), "debug.notFound") {
        t.Fatalf("expected the envelope to still be rendered, got %q", buffer.String())
    }
}

func TestRender_ReturnsNilWhenTheEnvelopeCarriesNoError(t *testing.T) {
    buffer := &bytes.Buffer{}

    envelope := newRendererTestEnvelope()

    renderErr := Render(buffer, envelope, Option{Format: FormatJson})
    if nil != renderErr {
        t.Fatalf("expected no error, got %v", renderErr)
    }

    if 0 == buffer.Len() {
        t.Fatalf("expected the envelope to be rendered")
    }
}

func TestRender_ReturnsNilForAnEnvelopeCarryingOnlyWarnings(t *testing.T) {
    buffer := &bytes.Buffer{}

    envelope := newRendererTestEnvelope()
    envelope.AddWarning(
        "debug.notSupported",
        "event dispatcher does not support inspection",
        nil,
    )

    renderErr := Render(buffer, envelope, Option{Format: FormatJson})
    if nil != renderErr {
        t.Fatalf("a warning must not fail the command, got %v", renderErr)
    }
}

func TestRender_ReturnsTheEnvelopeFailureUnmarked(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.SetError("cmd.failed", "the command failed", nil, nil)

    renderErr := Render(&bytes.Buffer{}, envelope, DefaultOption())
    if nil == renderErr {
        t.Fatalf("expected the exit-coded error")
    }

    var exitError *exception.ExitError
    if false == errors.As(renderErr, &exitError) {
        t.Fatalf("expected an ExitError, got %v", renderErr)
    }
    if 1 != exitError.ExitCode() {
        t.Fatalf("expected exit code 1, got %d", exitError.ExitCode())
    }
    if true == exitError.ErrorValue().AlreadyLogged() {
        t.Fatalf("expected the envelope failure to travel unmarked so the exit path logs it")
    }
}

func TestRender_LeavesTheUnnamedAndReservedDetailsOutOfTheReportedContext(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.SetError(
        "cmd.failed",
        "the command failed",
        map[string]any{
            "":          "filed under no name",
            "command":   "an impostor command",
            "errorCode": "an impostor code",
            "subject":   "order",
        },
        nil,
    )

    renderErr := Render(&bytes.Buffer{}, envelope, DefaultOption())

    var exitError *exception.ExitError
    if false == errors.As(renderErr, &exitError) {
        t.Fatalf("expected an ExitError, got %v", renderErr)
    }

    context := exitError.ErrorValue().Context()

    if _, unnamed := context[""]; true == unnamed {
        t.Fatalf("expected the unnamed detail to be left out, got %v", context)
    }
    if "cmd" != context["command"] {
        t.Fatalf("expected the command name to stay the framework's, got %v", context["command"])
    }
    if "cmd.failed" != context["errorCode"] {
        t.Fatalf("expected the error code to stay the framework's, got %v", context["errorCode"])
    }
    if "order" != context["subject"] {
        t.Fatalf("expected the named detail to be carried over, got %v", context["subject"])
    }
}

type failingRenderWriter struct {
}

func (instance *failingRenderWriter) Write(payload []byte) (int, error) {
    return 0, errors.New("disk full")
}

func TestRender_KeepsTheEnvelopeFailureWhenThePrintingFails(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.SetError("cmd.failed", "the business failure", nil, nil)

    renderErr := Render(&failingRenderWriter{}, envelope, DefaultOption())
    if nil == renderErr {
        t.Fatalf("expected an error")
    }

    var exitError *exception.ExitError
    if false == errors.As(renderErr, &exitError) {
        t.Fatalf("expected the envelope failure to survive as the exit-coded cause, got %v", renderErr)
    }
    if "the business failure" != exitError.ErrorValue().Message() {
        t.Fatalf("expected the business failure in the chain, got %q", exitError.ErrorValue().Message())
    }
    if false == strings.Contains(renderErr.Error(), "failed to print") {
        t.Fatalf("expected the print failure named on the outer error, got %q", renderErr.Error())
    }
}

func TestRender_ReturnsThePrintFailureAloneWhenTheEnvelopeCarriesNoError(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))

    renderErr := Render(&failingRenderWriter{}, envelope, Option{Format: FormatJson})
    if nil == renderErr {
        t.Fatalf("expected the print failure")
    }

    var exitError *exception.ExitError
    if true == errors.As(renderErr, &exitError) {
        t.Fatalf("expected no exit-coded wrapper without an envelope failure, got %v", renderErr)
    }
}
