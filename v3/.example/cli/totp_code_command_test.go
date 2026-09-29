package cli

import (
    "bytes"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/security/totp"
)

const totpCodeCommandTestSecret = "JBSWY3DPEHPK3PXP"

func TestTotpCodeCommandPrintsTheCodeOnTheCommandWriter(t *testing.T) {
    captured := &bytes.Buffer{}

    before, beforeErr := totp.GenerateCodeAt(totpCodeCommandTestSecret, time.Now(), totp.Config{})
    if nil != beforeErr {
        t.Fatalf("generate the expected code: %v", beforeErr)
    }

    runErr := NewTotpCodeCommand().Run(nil, newStringFlagContext(map[string]string{"secret": totpCodeCommandTestSecret}, captured))
    if nil != runErr {
        t.Fatalf("expected the code to be printed, got %v", runErr)
    }

    after, afterErr := totp.GenerateCodeAt(totpCodeCommandTestSecret, time.Now(), totp.Config{})
    if nil != afterErr {
        t.Fatalf("generate the expected code: %v", afterErr)
    }

    if output := captured.String(); before+"\n" != output && after+"\n" != output {
        t.Fatalf("expected the line to be exactly the code %q, got %q", before, output)
    }
}

func TestTotpCodeCommandAnswersARefusedWrite(t *testing.T) {
    refusal := errors.New("broken pipe")

    runErr := NewTotpCodeCommand().Run(nil, newStringFlagContext(map[string]string{"secret": totpCodeCommandTestSecret}, &refusingWriter{refusal: refusal}))
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}
