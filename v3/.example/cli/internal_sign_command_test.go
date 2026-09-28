package cli

import (
    "bytes"
    "errors"
    "strings"
    "testing"

    melodysecurity "github.com/precision-soft/melody/v3/security"
)

func newInternalSignTestSigner() *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        App: "wms-service",
        Secrets: melodysecurity.NewStaticHmacSecretProvider(
            "key-current",
            map[string]melodysecurity.HmacKey{
                "key-current": {App: "wms-service", Secret: []byte("internal-sign-command-test-secret")},
            },
        ),
    })
}

func TestInternalSignCommandPrintsTheHeaderNameAndValueOnTheCommandWriter(t *testing.T) {
    signer := newInternalSignTestSigner()
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(signer).Run(nil, &flagContext{writer: captured})
    if nil != runErr {
        t.Fatalf("expected the header to be minted, got %v", runErr)
    }

    lines := strings.Split(captured.String(), "\n")
    if 3 != len(lines) || "" != lines[2] {
        t.Fatalf("expected exactly two lines on the command writer, got %q", captured.String())
    }

    if signer.HeaderName() != lines[0] {
        t.Fatalf("expected the first line to be the header name %q, got %q", signer.HeaderName(), lines[0])
    }

    if "" == lines[1] {
        t.Fatalf("expected the second line to carry the signed header, got %q", captured.String())
    }
}

func TestInternalSignCommandAnswersARefusedWrite(t *testing.T) {
    refusal := errors.New("broken pipe")

    runErr := NewInternalSignCommand(newInternalSignTestSigner()).Run(nil, &flagContext{writer: &refusingWriter{refusal: refusal}})
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}
