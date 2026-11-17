package config

import (
    "bytes"
    "context"
    "os"
    "testing"
    "time"

    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    "github.com/precision-soft/melody/v4/security/totp"
)

func TestRegisterCliCommands_HandsTheTotpCodeCommandTheStandardInput(t *testing.T) {
    const secret = "JBSWY3DPEHPK3PXP"

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }
    t.Cleanup(func() { _ = readEnd.Close() })

    if _, writeErr := writeEnd.WriteString(secret + "\n"); nil != writeErr {
        t.Fatalf("write the secret: %v", writeErr)
    }
    _ = writeEnd.Close()

    standardInput := os.Stdin
    os.Stdin = readEnd
    t.Cleanup(func() { os.Stdin = standardInput })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })

    instant := time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewFrozenClock(instant), nil
    }, melodycontainer.RegisterOptions{})

    commandList := moduleWithEnvironment(t, map[string]string{}).RegisterCliCommands(&routeKernel{container: containerInstance})

    var totpCommand melodyclicontract.Command
    for _, command := range commandList {
        if "totp:code" == command.Name() {
            totpCommand = command
        }
    }

    if nil == totpCommand {
        t.Fatal("expected the totp:code command registered")
    }

    expected, expectedErr := totp.GenerateCodeAt(secret, instant, totp.Config{})
    if nil != expectedErr {
        t.Fatalf("generate the expected code: %v", expectedErr)
    }

    captured := &bytes.Buffer{}
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    if runErr := totpCommand.Run(runtimeInstance, &melodyclicontract.CommandContext{Writer: captured}); nil != runErr {
        t.Fatalf("expected the secret read from the standard input, got %v", runErr)
    }

    if expected+"\n" != captured.String() {
        t.Fatalf("expected the code %q, got %q", expected, captured.String())
    }
}
