package mailer

import (
    "bufio"
    "context"
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "math/big"
    "net"
    "strings"
    "testing"
    "time"

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

func TestSmtpTransport_AuthSucceedsWhenHostDiffersFromAddress(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    serverCertificate := generateSelfSignedCertificate(t)

    authResult := make(chan bool, 1)
    go serveStartTlsAuthSmtp(listener, serverCertificate, authResult)

    /* @important Address is an IP:port while Host is a different name (the TLS SNI / auth identity used when dialing by IP, through a tunnel, or a CNAME). smtp.PlainAuth rejects a mismatch between the client's server name and its host with "wrong host name", so the plain/STARTTLS dial path must build the client with instance.host — not derive the server name from the address, as smtp.Dial does. */
    transport := NewSmtpTransport(SmtpConfig{
        Address:     listener.Addr().String(),
        Host:        "smtp.internal.example",
        Username:    "user",
        Password:    "pass",
        RequireAuth: true,
        RequireTls:  true,
        TlsConfig:   &tls.Config{InsecureSkipVerify: true},
    })

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil != sendErr {
        t.Fatalf("expected authentication to succeed when Host differs from the Address host, got %v", sendErr)
    }

    select {
    case authenticated := <-authResult:
        if false == authenticated {
            t.Fatalf("expected the server to have accepted AUTH")
        }
    default:
        t.Fatalf("expected the server to have processed AUTH")
    }
}

func generateSelfSignedCertificate(t *testing.T) tls.Certificate {
    t.Helper()

    privateKey, keyErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    if nil != keyErr {
        t.Fatalf("generate key: %v", keyErr)
    }

    template := x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "smtp.internal.example"},
        NotBefore:    time.Now().Add(-time.Hour),
        NotAfter:     time.Now().Add(time.Hour),
        DNSNames:     []string{"smtp.internal.example"},
        IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
    }

    der, certErr := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
    if nil != certErr {
        t.Fatalf("create certificate: %v", certErr)
    }

    return tls.Certificate{
        Certificate: [][]byte{der},
        PrivateKey:  privateKey,
    }
}

func serveStartTlsAuthSmtp(listener net.Listener, certificate tls.Certificate, authResult chan<- bool) {
    connection, acceptErr := listener.Accept()
    if nil != acceptErr {
        authResult <- false
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
            authResult <- false
            return
        }

        command := strings.ToUpper(strings.TrimSpace(line))
        switch {
        case strings.HasPrefix(command, "EHLO") || strings.HasPrefix(command, "HELO"):
            writeLine("250-fake greets you")
            writeLine("250-STARTTLS")
            writeLine("250 AUTH PLAIN")
        case strings.HasPrefix(command, "STARTTLS"):
            writeLine("220 ready to start tls")

            tlsConnection := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}})
            if handshakeErr := tlsConnection.Handshake(); nil != handshakeErr {
                authResult <- false
                return
            }

            connection = tlsConnection
            reader = bufio.NewReader(tlsConnection)
            writeLine = func(line string) {
                tlsConnection.Write([]byte(line + "\r\n"))
            }
        case strings.HasPrefix(command, "AUTH PLAIN"):
            authResult <- true
            writeLine("235 2.7.0 accepted")
        case strings.HasPrefix(command, "MAIL"):
            writeLine("250 ok")
        case strings.HasPrefix(command, "RCPT"):
            writeLine("250 ok")
        case strings.HasPrefix(command, "DATA"):
            writeLine("354 end with .")
            for {
                dataLine, dataErr := reader.ReadString('\n')
                if nil != dataErr {
                    return
                }
                if ".\r\n" == dataLine || ".\n" == dataLine {
                    break
                }
            }
            writeLine("250 queued")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}

/** @info smtp.NewClient reads the server's 220 greeting synchronously with no deadline, so a server that accepts the connection and then says nothing pinned the sending goroutine and its socket forever. */
func TestSmtpTransport_DialTimesOutWhenTheServerNeverGreets(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    accepted := make(chan struct{})

    go func() {
        connection, acceptErr := listener.Accept()
        if nil != acceptErr {
            return
        }

        close(accepted)

        /* accept, then stay silent: no 220 greeting ever arrives */
        <-time.After(5 * time.Second)
        connection.Close()
    }()

    transport := NewSmtpTransport(SmtpConfig{
        Address:     listener.Addr().String(),
        Host:        "127.0.0.1",
        DialTimeout: 150 * time.Millisecond,
    })

    finished := make(chan error, 1)
    go func() {
        _, _, dialErr := transport.dial()
        finished <- dialErr
    }()

    <-accepted

    select {
    case dialErr := <-finished:
        if nil == dialErr {
            t.Fatal("expected the silent server to produce a dial error")
        }
    case <-time.After(3 * time.Second):
        t.Fatal("dial hung on a server that accepted the connection and never sent a greeting")
    }
}

/** @info the greeting deadline only bounds the opening handshake; a relay that greets promptly then stalls mid-session (here on DATA) must still be bounded by the per-step session deadline, or the sending goroutine hangs forever. */
func TestSmtpTransport_TimesOutWhenServerStallsMidSession(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    released := make(chan struct{})
    defer close(released)

    go serveStallOnDataSmtp(listener, released)

    transport := NewSmtpTransport(SmtpConfig{
        Address: listener.Addr().String(),
        Host:    "127.0.0.1",
        Timeout: 150 * time.Millisecond,
    })

    finished := make(chan error, 1)
    go func() {
        finished <- transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })
    }()

    select {
    case sendErr := <-finished:
        if nil == sendErr {
            t.Fatal("expected a timeout error when the server stalls mid-session")
        }
    case <-time.After(3 * time.Second):
        t.Fatal("send hung on a server that greeted then stalled mid-session")
    }
}

/** @info net/smtp has no context api, so a cancelled runtime context can only reach an in-flight command by closing the connection; the session timeout here is long, so only the cancellation can unblock the stalled DATA. */
func TestSmtpTransport_ContextCancellationAbortsSession(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    released := make(chan struct{})
    defer close(released)

    go serveStallOnDataSmtp(listener, released)

    transport := NewSmtpTransport(SmtpConfig{
        Address: listener.Addr().String(),
        Host:    "127.0.0.1",
        Timeout: 10 * time.Second,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- transport.Send(testRuntimeWithContext(ctx), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })
    }()

    go func() {
        <-time.After(200 * time.Millisecond)
        cancel()
    }()

    select {
    case sendErr := <-finished:
        if nil == sendErr {
            t.Fatal("expected an error when the runtime context is cancelled mid-session")
        }
    case <-time.After(3 * time.Second):
        t.Fatal("send ignored the cancelled runtime context and hung mid-session")
    }
}

/* serveStallOnDataSmtp greets, completes EHLO and accepts MAIL/RCPT, then stalls after the client issues DATA — it never sends the 354 continuation — until released is closed or a safety timeout elapses, modelling a relay that black-holes traffic once the conversation is under way. */
func serveStallOnDataSmtp(listener net.Listener, released <-chan struct{}) {
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
        case strings.HasPrefix(command, "MAIL"):
            writeLine("250 ok")
        case strings.HasPrefix(command, "RCPT"):
            writeLine("250 ok")
        case strings.HasPrefix(command, "DATA"):
            select {
            case <-released:
            case <-time.After(5 * time.Second):
            }

            return
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}
