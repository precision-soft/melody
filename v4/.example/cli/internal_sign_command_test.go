package cli

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

func newInternalSignTestSigner(ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        App: "wms-service",
        Ttl: ttl,
        Secrets: melodysecurity.NewStaticHmacSecretProvider(melodysecurity.HmacKeyRing{
            CurrentKeyId: "key-current",
            Keys: map[string]melodysecurity.HmacKey{
                "key-current": {App: "wms-service", Secret: []byte("internal-sign-command-test-secret")},
            },
        }),
    })
}

func TestInternalSignCommandPrintsTheHeaderNameAndValueOnTheCommandWriter(t *testing.T) {
    signer := newInternalSignTestSigner(bound.Duration{})
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(nil, captured))
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

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(nil, &refusingWriter{refusal: refusal}))
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}

func TestInternalSignCommandHandsTheSignerTheTtlAsked(t *testing.T) {
    cases := map[string]bound.Duration{
        "":    {},
        "1h":  bound.Of(time.Hour),
        "45s": bound.Of(45 * time.Second),
    }

    for rawTtl, expected := range cases {
        handed := bound.Of(time.Duration(-1))
        newSigner := func(ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner {
            handed = ttl

            return newInternalSignTestSigner(ttl)
        }

        runErr := NewInternalSignCommand(newSigner).Run(nil, newStringFlagContext(map[string]string{"ttl": rawTtl}, &bytes.Buffer{}))
        if nil != runErr || expected != handed {
            t.Fatalf("expected --ttl %q to hand the signer %s, got %s and %v", rawTtl, expected, handed, runErr)
        }
    }
}

func TestInternalSignCommandRefusesATtlThatIsNotADurationOfAtLeastASecond(t *testing.T) {
    for _, rawTtl := range []string{"soon", "-1s", "0s", "500ms"} {
        runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"ttl": rawTtl}, &bytes.Buffer{}))
        if nil == runErr || false == strings.Contains(runErr.Error(), "is not a go duration of at least 1s") {
            t.Fatalf("expected --ttl %q refused, got %v", rawTtl, runErr)
        }
    }
}
