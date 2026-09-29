package cli

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"

    melodysecurity "github.com/precision-soft/melody/v3/security"
)

func newInternalSignTestSigner(ttl time.Duration) *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        App: "wms-service",
        Ttl: ttl,
        Secrets: melodysecurity.NewStaticHmacSecretProvider(
            "key-current",
            map[string]melodysecurity.HmacKey{
                "key-current": {App: "wms-service", Secret: []byte("internal-sign-command-test-secret")},
            },
        ),
    })
}

func TestInternalSignCommandPrintsTheHeaderNameAndValueOnTheCommandWriter(t *testing.T) {
    signer := newInternalSignTestSigner(0)
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
    cases := map[string]time.Duration{
        "":    0,
        "1h":  time.Hour,
        "45s": 45 * time.Second,
    }

    for rawTtl, expected := range cases {
        handed := time.Duration(-1)
        newSigner := func(ttl time.Duration) *melodysecurity.HmacEnvelopeSigner {
            handed = ttl

            return newInternalSignTestSigner(ttl)
        }

        runErr := NewInternalSignCommand(newSigner).Run(nil, newStringFlagContext(map[string]string{"ttl": rawTtl}, &bytes.Buffer{}))
        if nil != runErr || expected != handed {
            t.Fatalf("expected --ttl %q to hand the signer %s, got %s and %v", rawTtl, expected, handed, runErr)
        }
    }
}

func TestInternalSignCommandRefusesATtlThatIsNotAPositiveDuration(t *testing.T) {
    for _, rawTtl := range []string{"soon", "-1s", "0s"} {
        runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"ttl": rawTtl}, &bytes.Buffer{}))
        if nil == runErr || false == strings.Contains(runErr.Error(), "is not a positive go duration") {
            t.Fatalf("expected --ttl %q refused, got %v", rawTtl, runErr)
        }
    }
}
