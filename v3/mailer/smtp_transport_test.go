package mailer

import (
    "bufio"
    "net"
    "strings"
    "testing"

    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

func TestSmtpTransport_RequireAuthFailsWhenServerHasNoAuthExtension(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := NewSmtpTransport(SmtpConfig{
        Address:     listener.Addr().String(),
        Username:    "user",
        Password:    "pass",
        RequireAuth: true,
    })

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected RequireAuth to fail when the server does not advertise AUTH")
    }

    if false == strings.Contains(sendErr.Error(), "AUTH") {
        t.Fatalf("expected an AUTH-related error, got %v", sendErr)
    }
}

func TestSmtpTransport_RequireAuthFailsWhenNoUsernameConfigured(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := NewSmtpTransport(SmtpConfig{
        Address:     listener.Addr().String(),
        RequireAuth: true,
    })

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected RequireAuth to fail closed when no username is configured")
    }

    if false == strings.Contains(sendErr.Error(), "username") {
        t.Fatalf("expected a missing-username error, got %v", sendErr)
    }
}

func serveAuthlessSmtp(listener net.Listener) {
    connection, acceptErr := listener.Accept()
    if nil != acceptErr {
        return
    }
    defer connection.Close()

    reader := bufio.NewReader(connection)
    writeLine := func(line string) {
        connection.Write([]byte(line + "\r\n"))
    }

    writeLine("220 fake ESMTP")

    for {
        line, readErr := reader.ReadString('\n')
        if nil != readErr {
            return
        }

        command := strings.ToUpper(strings.TrimSpace(line))
        switch {
        case strings.HasPrefix(command, "EHLO") || strings.HasPrefix(command, "HELO"):
            writeLine("250-fake greets you")
            writeLine("250 SIZE 35882577")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}
