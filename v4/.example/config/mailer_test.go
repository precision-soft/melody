package config

import (
    "bufio"
    "context"
    "encoding/base64"
    "errors"
    "fmt"
    "net"
    "strings"
    "sync"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodymailercontract "github.com/precision-soft/melody/v4/mailer/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
)

func TestSmtpConfig_CarriesTheCredentialsAndRequiresAuthentication(t *testing.T) {
    smtpConfig := moduleWithEnvironment(t, map[string]string{
        environmentKeySmtpAddress:  "relay.example.test:587",
        environmentKeySmtpUsername: "mailer",
        environmentKeySmtpPassword: "relay-password",
    }).smtpConfig()

    if "relay.example.test:587" != smtpConfig.Address || "mailer" != smtpConfig.Username || "relay-password" != smtpConfig.Password || false == smtpConfig.RequireAuthentication {
        t.Fatalf("expected the relay's credentials and authentication required, got %+v", smtpConfig)
    }

    anonymous := moduleWithEnvironment(t, map[string]string{environmentKeySmtpAddress: "mailpit:1025"}).smtpConfig()
    if true == anonymous.RequireAuthentication || "" != anonymous.Username {
        t.Fatalf("expected a relay without a username to authenticate nothing, got %+v", anonymous)
    }
}

func TestSmtpConfig_ReadsTheTwoTlsSwitches(t *testing.T) {
    requireTls := moduleWithEnvironment(t, map[string]string{environmentKeySmtpAddress: "relay.example.test:587", environmentKeySmtpRequireTls: "true"}).smtpConfig()
    if false == requireTls.RequireTls || true == requireTls.ImplicitTls {
        t.Fatalf("expected STARTTLS required alone, got %+v", requireTls)
    }

    implicitTls := moduleWithEnvironment(t, map[string]string{environmentKeySmtpAddress: "relay.example.test:465", environmentKeySmtpImplicitTls: "true", environmentKeySmtpRequireTls: "false"}).smtpConfig()
    if true == implicitTls.RequireTls || false == implicitTls.ImplicitTls {
        t.Fatalf("expected implicit tls alone, got %+v", implicitTls)
    }

    neither := moduleWithEnvironment(t, map[string]string{}).smtpConfig()
    if true == neither.RequireTls || true == neither.ImplicitTls {
        t.Fatalf("expected both switches off when empty, got %+v", neither)
    }
}

func TestSmtpConfig_RefusesAPasswordWithoutAUsername(t *testing.T) {
    module := moduleWithEnvironment(t, map[string]string{environmentKeySmtpAddress: "relay.example.test:587", environmentKeySmtpPassword: "relay-password"})

    refusal := recoveredRefusal(t, func() { module.smtpConfig() })
    if nil == refusal || false == strings.Contains(refusal.Error(), "the smtp password is set without a username") {
        t.Fatalf("expected a password without a username refused, got %v", refusal)
    }

    if true == strings.Contains(refusal.Error(), "relay-password") {
        t.Fatal("expected the refusal to keep the password out")
    }
}

func TestSmtpConfig_RefusesASwitchThatIsNotABoolean(t *testing.T) {
    for _, environmentKey := range []string{environmentKeySmtpRequireTls, environmentKeySmtpImplicitTls} {
        module := moduleWithEnvironment(t, map[string]string{environmentKeySmtpAddress: "relay.example.test:587", environmentKey: "yes"})

        refusal := recoveredRefusal(t, func() { module.smtpConfig() })
        if nil == refusal || false == strings.Contains(refusal.Error(), "the switch is neither true nor false") {
            t.Fatalf("expected %s=yes refused, got %v", environmentKey, refusal)
        }
    }
}

func TestSmtpConfig_RefusesACombinationTheTransportWouldNotHonour(t *testing.T) {
    refusals := []struct {
        environment map[string]string
        message     string
        key         string
    }{
        {map[string]string{environmentKeySmtpAddress: "relay.example.test:587", environmentKeySmtpUsername: "mailer"}, "the smtp username is set without a password", environmentKeySmtpUsername},
        {map[string]string{environmentKeySmtpAddress: "relay.example.test:465", environmentKeySmtpRequireTls: "true", environmentKeySmtpImplicitTls: "true"}, "the smtp tls switches are both on", environmentKeySmtpRequireTls},
        {map[string]string{environmentKeySmtpUsername: "mailer", environmentKeySmtpPassword: "relay-password"}, "the smtp relay setting is set without an address", environmentKeySmtpUsername},
        {map[string]string{environmentKeySmtpRequireTls: "true"}, "the smtp relay setting is set without an address", environmentKeySmtpRequireTls},
        {map[string]string{environmentKeySmtpImplicitTls: "false"}, "the smtp relay setting is set without an address", environmentKeySmtpImplicitTls},
    }

    for _, refusalCase := range refusals {
        module := moduleWithEnvironment(t, refusalCase.environment)

        refusal := recoveredRefusal(t, func() { module.smtpConfig() })
        if nil == refusal || false == strings.Contains(refusal.Error(), refusalCase.message) {
            t.Fatalf("expected %v refused with %q, got %v", refusalCase.environment, refusalCase.message, refusal)
        }

        var refusalError *melodyexception.Error
        if false == errors.As(refusal, &refusalError) || refusalCase.key != refusalError.Context()["key"] {
            t.Fatalf("expected %v refused naming %s, got %v", refusalCase.environment, refusalCase.key, refusal)
        }

        if true == strings.Contains(refusal.Error(), "relay-password") {
            t.Fatal("expected the refusal to keep the password out")
        }
    }
}

type mailRecordingLogger struct {
    melodyloggingcontract.Logger
    mutex       sync.Mutex
    messageList []string
}

func (instance *mailRecordingLogger) Info(message string, context melodyloggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messageList = append(instance.messageList, message)
}

func TestBuildMailer_WiresTheLogTransportWithoutAnAddress(t *testing.T) {
    module := moduleWithEnvironment(t, map[string]string{})
    module.buildMailer()

    logger := &mailRecordingLogger{Logger: melodylogging.NewNopLogger()}
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return logger, nil
    }, melodycontainer.RegisterOptions{})

    sendErr := module.mailer.Send(melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance), melodymailercontract.Message{
        From:    melodymailercontract.Address{Email: "catalog@example.test"},
        To:      []melodymailercontract.Address{{Email: "reader@example.test"}},
        Subject: "welcome",
        Text:    "welcome",
    })
    if nil != sendErr {
        t.Fatalf("expected the log transport to take the message, got %v", sendErr)
    }

    if 1 != len(logger.messageList) || "mailer log transport captured a message" != logger.messageList[0] {
        t.Fatalf("expected the message captured in the journal, got %v", logger.messageList)
    }
}

type smtpRelayStub struct {
    listener       net.Listener
    mutex          sync.Mutex
    authentication string
}

func newSmtpRelayStub(t *testing.T) *smtpRelayStub {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    t.Cleanup(func() { _ = listener.Close() })

    relay := &smtpRelayStub{listener: listener}
    go relay.serve()

    return relay
}

func (instance *smtpRelayStub) serve() {
    connection, acceptErr := instance.listener.Accept()
    if nil != acceptErr {
        return
    }
    defer connection.Close()

    reader := bufio.NewReader(connection)
    _, _ = fmt.Fprint(connection, "220 relay.test ready\r\n")

    for {
        line, readErr := reader.ReadString('\n')
        if nil != readErr {
            return
        }

        command := strings.TrimSpace(line)
        switch {
        case true == strings.HasPrefix(command, "EHLO"):
            _, _ = fmt.Fprint(connection, "250-relay.test\r\n250 AUTH PLAIN\r\n")
        case true == strings.HasPrefix(command, "AUTH PLAIN "):
            instance.mutex.Lock()
            instance.authentication = strings.TrimPrefix(command, "AUTH PLAIN ")
            instance.mutex.Unlock()

            _, _ = fmt.Fprint(connection, "535 refused by the stub\r\n")
        default:
            _, _ = fmt.Fprint(connection, "221 bye\r\n")

            return
        }
    }
}

func sendThroughBuiltMailer(t *testing.T, environment map[string]string) error {
    t.Helper()

    module := moduleWithEnvironment(t, environment)
    module.buildMailer()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    }, melodycontainer.RegisterOptions{})

    return module.mailer.Send(melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance), melodymailercontract.Message{
        From:    melodymailercontract.Address{Email: "catalog@example.test"},
        To:      []melodymailercontract.Address{{Email: "reader@example.test"}},
        Subject: "welcome",
        Text:    "welcome",
    })
}

func TestBuildMailer_HandsTheRelayTheConfiguredCredentials(t *testing.T) {
    relay := newSmtpRelayStub(t)

    sendErr := sendThroughBuiltMailer(t, map[string]string{
        environmentKeySmtpAddress:  relay.listener.Addr().String(),
        environmentKeySmtpUsername: "mailer",
        environmentKeySmtpPassword: "relay-password",
    })
    if nil == sendErr {
        t.Fatal("expected the stub's auth refusal answered")
    }

    relay.mutex.Lock()
    defer relay.mutex.Unlock()

    decoded, decodeErr := base64.StdEncoding.DecodeString(relay.authentication)
    if nil != decodeErr || "\x00mailer\x00relay-password" != string(decoded) {
        t.Fatalf("expected the relay to receive the configured credentials, got %q (%v)", decoded, decodeErr)
    }
}

func TestBuildMailer_HandsTheRelayTheRequireTlsSwitch(t *testing.T) {
    relay := newSmtpRelayStub(t)

    sendErr := sendThroughBuiltMailer(t, map[string]string{
        environmentKeySmtpAddress:    relay.listener.Addr().String(),
        environmentKeySmtpRequireTls: "true",
    })
    if nil == sendErr || false == strings.Contains(sendErr.Error(), "tls is required") {
        t.Fatalf("expected a relay without STARTTLS refused under the switch, got %v", sendErr)
    }
}
