package cli

import (
    "bytes"
    "context"
    "errors"
    "io"
    "strings"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/security/totp"
)

const totpCodeCommandTestSecret = "JBSWY3DPEHPK3PXP"

func TestTotpCodeCommand_ReadsTheSecretFromItsInput(t *testing.T) {
    instant := time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)
    captured := &bytes.Buffer{}

    expected, expectedErr := totp.GenerateCodeAt(totpCodeCommandTestSecret, instant, totp.Config{})
    if nil != expectedErr {
        t.Fatalf("generate the expected code: %v", expectedErr)
    }

    runErr := NewTotpCodeCommand(strings.NewReader(totpCodeCommandTestSecret+"\n")).Run(runtimeWithClock(t, melodyclock.NewFrozenClock(instant)), parsedContextValues{WriterValue: captured}.parse())
    if nil != runErr {
        t.Fatalf("expected the code to be printed, got %v", runErr)
    }

    if expected+"\n" != captured.String() {
        t.Fatalf("expected the line to be exactly the code %q, got %q", expected, captured.String())
    }
}

func TestTotpCodeCommand_RefusesAnEmptyInput(t *testing.T) {
    for _, input := range []string{"", "\n", "   \n"} {
        captured := &bytes.Buffer{}

        runErr := NewTotpCodeCommand(strings.NewReader(input)).Run(runtimeWithClock(t, melodyclock.NewSystemClock()), parsedContextValues{WriterValue: captured}.parse())
        if nil == runErr || false == strings.Contains(runErr.Error(), "a secret is required on standard input") {
            t.Fatalf("expected an empty input %q refused, got %v", input, runErr)
        }

        if "" != captured.String() {
            t.Fatalf("expected nothing printed for an empty input, got %q", captured.String())
        }
    }
}

func TestTotpCodeCommand_DeclaresNoSecretFlag(t *testing.T) {
    for _, flag := range NewTotpCodeCommand(nil).Flags() {
        for _, name := range flag.Names() {
            if "secret" == name {
                t.Fatal("expected no --secret flag, the secret is read from standard input")
            }
        }
    }
}

func TestTotpCodeCommand_GeneratesAtTheContainersClock(t *testing.T) {
    clockInstance := melodyclock.NewFrozenClock(time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC))
    runtimeInstance := runtimeWithClock(t, clockInstance)

    codeAt := func() string {
        captured := &bytes.Buffer{}

        if runErr := NewTotpCodeCommand(strings.NewReader(totpCodeCommandTestSecret)).Run(runtimeInstance, parsedContextValues{WriterValue: captured}.parse()); nil != runErr {
            t.Fatalf("run: %v", runErr)
        }

        return strings.TrimSpace(captured.String())
    }

    first := codeAt()
    firstExpected, _ := totp.GenerateCodeAt(totpCodeCommandTestSecret, clockInstance.Now(), totp.Config{})

    clockInstance.Advance(30 * time.Second)

    second := codeAt()
    secondExpected, _ := totp.GenerateCodeAt(totpCodeCommandTestSecret, clockInstance.Now(), totp.Config{})

    if firstExpected != first || secondExpected != second || first == second {
        t.Fatalf("expected the codes of the two frozen instants %q and %q, got %q and %q", firstExpected, secondExpected, first, second)
    }
}

func TestTotpCodeCommandAnswersARefusedWrite(t *testing.T) {
    refusal := errors.New("broken pipe")

    runErr := NewTotpCodeCommand(strings.NewReader(totpCodeCommandTestSecret)).Run(runtimeWithClock(t, melodyclock.NewSystemClock()), parsedContextValues{WriterValue: &refusingWriter{refusal: refusal}}.parse())
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}

func TestTotpCodeCommand_GivesUpTheReadWhenTheRuntimeContextEnds(t *testing.T) {
    readEnd, writeEnd := io.Pipe()
    t.Cleanup(func() { _ = writeEnd.Close() })

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    clockRuntime := runtimeWithClock(t, melodyclock.NewSystemClock())
    runtimeInstance := melodyruntime.WithContext(clockRuntime, ctx)
    resultChannel := make(chan error, 1)

    go func() {
        resultChannel <- NewTotpCodeCommand(readEnd).Run(runtimeInstance, parsedContextValues{}.parse())
    }()

    select {
    case runErr := <-resultChannel:
        if false == errors.Is(runErr, context.Canceled) {
            t.Fatalf("expected the ended context answered, got %v", runErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the read given up once the runtime context ended")
    }
}
