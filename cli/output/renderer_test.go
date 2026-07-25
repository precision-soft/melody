package output

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/exception"
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
