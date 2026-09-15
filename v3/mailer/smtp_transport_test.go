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
    "errors"
    "math/big"
    "net"
    "strings"
    "syscall"
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
        finished <- transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })
    }()

    <-accepted

    select {
    case sendErr := <-finished:
        if nil == sendErr {
            t.Fatal("expected the silent server to produce a dial error")
        }
    case <-time.After(3 * time.Second):
        t.Fatal("dial hung on a server that accepted the connection and never sent a greeting")
    }
}

func TestSmtpTransport_CancellationInterruptsTheGreetingRead(t *testing.T) {
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

        <-time.After(10 * time.Second)
        connection.Close()
    }()

    transport := NewSmtpTransport(SmtpConfig{
        Address:     listener.Addr().String(),
        Host:        "127.0.0.1",
        DialTimeout: 10 * time.Second,
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

    <-accepted

    go func() {
        <-time.After(200 * time.Millisecond)
        cancel()
    }()

    select {
    case sendErr := <-finished:
        if nil == sendErr {
            t.Fatal("expected an error when the runtime context is cancelled during the greeting read")
        }
    case <-time.After(3 * time.Second):
        t.Fatal("send ignored the cancelled runtime context and stalled in the greeting read")
    }
}

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

func TestSmtpTransport_ImplicitTlsTimesOutWhenServerStallsAfterGreeting(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    released := make(chan struct{})
    defer close(released)

    serverCertificate := generateSelfSignedCertificate(t)
    go serveImplicitTlsGreetThenStallSmtp(listener, serverCertificate, released)

    transport := NewSmtpTransport(SmtpConfig{
        Address:     listener.Addr().String(),
        Host:        "127.0.0.1",
        Username:    "user",
        Password:    "pass",
        ImplicitTls: true,
        TlsConfig:   &tls.Config{InsecureSkipVerify: true},
        Timeout:     200 * time.Millisecond,
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
            t.Fatal("expected a timeout error when the implicit-tls server stalls after the greeting")
        }
    case <-time.After(2 * time.Second):
        t.Fatal("send hung on an implicit-tls server that greeted then went silent before the hello")
    }
}

func serveImplicitTlsGreetThenStallSmtp(listener net.Listener, certificate tls.Certificate, released <-chan struct{}) {
    connection, acceptErr := listener.Accept()
    if nil != acceptErr {
        return
    }
    defer connection.Close()

    tlsConnection := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}})
    if handshakeErr := tlsConnection.Handshake(); nil != handshakeErr {
        return
    }
    defer tlsConnection.Close()

    tlsConnection.Write([]byte("220 fake ESMTP\r\n"))

    select {
    case <-released:
    case <-time.After(5 * time.Second):
    }
}

func TestSmtpTransport_SendWithNilRuntimeReturnsError(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := NewSmtpTransport(SmtpConfig{
        Address: listener.Addr().String(),
    })

    sendErr := transport.Send(nil, mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatal("expected an error when the runtime is nil")
    }

    if false == strings.Contains(sendErr.Error(), "runtime may not be nil") {
        t.Fatalf("expected the nil-runtime error, got %v", sendErr)
    }
}

func TestSmtpTransport_ResolveTlsConfigDefaultsServerNameOnUserConfig(t *testing.T) {
    userConfig := &tls.Config{RootCAs: x509.NewCertPool()}

    transport := NewSmtpTransport(SmtpConfig{
        Address:   "203.0.113.10:465",
        Host:      "smtp.internal.example",
        TlsConfig: userConfig,
    })

    resolved := transport.resolveTlsConfig()

    if "smtp.internal.example" != resolved.ServerName {
        t.Fatalf("expected the resolved config to default ServerName to the transport host, got %q", resolved.ServerName)
    }

    if userConfig.RootCAs != resolved.RootCAs {
        t.Fatal("expected the resolved config to keep the user's RootCAs")
    }

    if "" != userConfig.ServerName {
        t.Fatalf("expected the user's config to remain unmodified, got ServerName %q", userConfig.ServerName)
    }
}

func TestSmtpTransport_ResolveTlsConfigKeepsInsecureSkipVerifyConfigVerbatim(t *testing.T) {
    userConfig := &tls.Config{InsecureSkipVerify: true}

    transport := NewSmtpTransport(SmtpConfig{
        Address:   "203.0.113.10:465",
        TlsConfig: userConfig,
    })

    if userConfig != transport.resolveTlsConfig() {
        t.Fatal("expected the insecure-skip-verify config to be returned verbatim")
    }
}

func TestSmtpTransport_SendsLargePayloadToSlowButSteadyReader(t *testing.T) {
    listener := listenWithSmallReceiveBuffer(t)
    defer listener.Close()

    go serveSlowSteadyDataSmtp(listener)

    transport := NewSmtpTransport(SmtpConfig{
        Address: listener.Addr().String(),
        Host:    "127.0.0.1",
        Timeout: 500 * time.Millisecond,
    })

    finished := make(chan error, 1)
    go func() {
        finished <- transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    strings.Repeat("melody carries a large body across a slow but steady link\n", 140000),
        })
    }()

    select {
    case sendErr := <-finished:
        if nil != sendErr {
            t.Fatalf("expected the slow-but-steady reader to receive the large payload, got %v", sendErr)
        }
    case <-time.After(30 * time.Second):
        t.Fatal("send hung on a server that drained the payload slowly but steadily")
    }
}

func TestSmtpTransport_TimesOutWhenServerStopsReadingMidPayload(t *testing.T) {
    listener := listenWithSmallReceiveBuffer(t)
    defer listener.Close()

    released := make(chan struct{})
    defer close(released)

    stalled := make(chan struct{})

    go serveStallMidDataSmtp(listener, released, stalled)

    const sessionTimeout = 500 * time.Millisecond

    transport := NewSmtpTransport(SmtpConfig{
        Address: listener.Addr().String(),
        Host:    "127.0.0.1",
        Timeout: sessionTimeout,
    })

    finished := make(chan error, 1)
    go func() {
        finished <- transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    strings.Repeat("melody carries a large body across a slow but steady link\n", 140000),
        })
    }()

    select {
    case <-stalled:
    case sendErr := <-finished:
        t.Fatalf("the send ended before the server stalled, so the stall was never exercised: %v", sendErr)
    case <-time.After(60 * time.Second):
        t.Fatal("the server never reached the stall point")
    }

    stalledAt := time.Now()

    const cutBound = 8 * sessionTimeout

    select {
    case sendErr := <-finished:
        if nil == sendErr {
            t.Fatal("expected a timeout error when the server stops reading mid-payload")
        }

        var netErr net.Error
        if false == errors.As(sendErr, &netErr) || false == netErr.Timeout() {
            t.Fatalf("expected an i/o timeout error, got %v", sendErr)
        }

        if cutSince := time.Since(stalledAt); cutBound < cutSince {
            t.Fatalf("expected the session to be cut within %v of the stall, took %v", cutBound, cutSince)
        }
    case <-time.After(60 * time.Second):
        t.Fatal("send was not cut on a server that stopped reading mid-payload")
    }
}

func listenWithSmallReceiveBuffer(t *testing.T) net.Listener {
    t.Helper()

    listenConfig := net.ListenConfig{
        Control: func(network string, address string, rawConnection syscall.RawConn) error {
            var optionErr error
            controlErr := rawConnection.Control(func(descriptor uintptr) {
                optionErr = syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 16384)
            })
            if nil != controlErr {
                return controlErr
            }

            return optionErr
        },
    }

    listener, listenErr := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }

    return listener
}

func serveSlowSteadyDataSmtp(listener net.Listener) {
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
            writeLine("354 end with .")

            buffer := make([]byte, 16*1024)
            tail := make([]byte, 0, 5)
            drained := 0
            for {
                count, dataErr := reader.Read(buffer)
                if count > 0 {
                    drained += count
                    tail = append(tail, buffer[:count]...)
                    if len(tail) > 5 {
                        tail = tail[len(tail)-5:]
                    }
                }
                if nil != dataErr {
                    return
                }
                if "\r\n.\r\n" == string(tail) {
                    break
                }

                if drained < 5*1024*1024 {
                    time.Sleep(2 * time.Millisecond)
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

func serveStallMidDataSmtp(listener net.Listener, released <-chan struct{}, stalled chan<- struct{}) {
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
            writeLine("354 end with .")

            buffer := make([]byte, 16*1024)
            drained := 0
            for drained < 64*1024 {
                count, dataErr := reader.Read(buffer)
                if nil != dataErr {
                    return
                }
                drained += count
            }

            close(stalled)

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

func TestSmtpTransport_DataTerminationTimeoutDerivation(t *testing.T) {
    cases := []struct {
        name     string
        config   SmtpConfig
        expected time.Duration
    }{
        {
            name:     "tight per-step timeout is floored at two minutes",
            config:   SmtpConfig{Address: "smtp:25", Timeout: 5 * time.Second},
            expected: 2 * time.Minute,
        },
        {
            name:     "wide per-step timeout derives four steps",
            config:   SmtpConfig{Address: "smtp:25", Timeout: 40 * time.Second},
            expected: 160 * time.Second,
        },
        {
            name:     "explicit value wins over the derivation",
            config:   SmtpConfig{Address: "smtp:25", Timeout: 40 * time.Second, DataTerminationTimeout: 90 * time.Second},
            expected: 90 * time.Second,
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            transport := NewSmtpTransport(testCase.config)
            if testCase.expected != transport.dataTerminationTimeout {
                t.Fatalf("expected the dot-acknowledgment ceiling %v, got %v", testCase.expected, transport.dataTerminationTimeout)
            }
        })
    }
}

func TestSmtpTransport_SlowDotAcknowledgmentSucceedsWithinItsOwnCeiling(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveDelayedDotAcknowledgmentSmtp(listener, 700*time.Millisecond)

    transport := NewSmtpTransport(SmtpConfig{
        Address:                listener.Addr().String(),
        Host:                   "127.0.0.1",
        Timeout:                200 * time.Millisecond,
        DataTerminationTimeout: 5 * time.Second,
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
        if nil != sendErr {
            t.Fatalf("expected the slow dot acknowledgment to succeed under its own ceiling, got %v", sendErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("send hung on a server that delayed only the dot acknowledgment")
    }
}

func serveDelayedDotAcknowledgmentSmtp(listener net.Listener, delay time.Duration) {
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

    inData := false

    for {
        line, readErr := reader.ReadString('\n')
        if nil != readErr {
            return
        }

        if true == inData {
            if "." == strings.TrimRight(line, "\r\n") {
                time.Sleep(delay)
                writeLine("250 queued")
                inData = false
            }

            continue
        }

        command := strings.ToUpper(strings.TrimSpace(line))
        switch {
        case strings.HasPrefix(command, "EHLO") || strings.HasPrefix(command, "HELO"):
            writeLine("250-fake greets you")
            writeLine("250 SIZE 35882577")
        case strings.HasPrefix(command, "DATA"):
            writeLine("354 end with .")
            inData = true
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}

func TestSmtpTransport_CancelledContextAbortsDial(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    transport := NewSmtpTransport(SmtpConfig{
        Address:     "203.0.113.1:25",
        Host:        "203.0.113.1",
        DialTimeout: 5 * time.Second,
    })

    finished := make(chan error, 1)
    go func() {
        finished <- transport.Send(testRuntimeWithContext(ctx), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })
    }()

    select {
    case sendErr := <-finished:
        if nil == sendErr {
            t.Fatal("expected the cancelled context to abort the dial with an error")
        }
    case <-time.After(2 * time.Second):
        t.Fatal("send ignored the cancelled context and stalled in the dial")
    }
}

func TestSmtpTransport_ConfiguredCredentialsFailClosedWhenAuthIsNotAdvertised(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := NewSmtpTransport(SmtpConfig{
        Address:  listener.Addr().String(),
        Username: "user",
        Password: "pass",
    })

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected configured credentials to fail closed when the server does not advertise AUTH")
    }

    if false == strings.Contains(sendErr.Error(), "while credentials are configured") {
        t.Fatalf("expected the refusal to name the unapplied credentials, got %v", sendErr)
    }
}

func TestSmtpTransport_SendWithTypedNilRuntimeReturnsError(t *testing.T) {
    transport := NewSmtpTransport(SmtpConfig{Address: "127.0.0.1:0"})

    var typedNil *nilRuntime

    sendErr := transport.Send(typedNil, mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
    })
    if nil == sendErr || false == strings.Contains(sendErr.Error(), "runtime may not be nil") {
        t.Fatalf("expected the typed-nil runtime to be refused, got %v", sendErr)
    }
}

func serveMailRejectingSmtp(listener net.Listener) {
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
            writeLine("550 sender refused")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}

func TestSmtpTransport_MailCommandFailureNamesTheCommandNotAVerdict(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveMailRejectingSmtp(listener)

    transport := NewSmtpTransport(SmtpConfig{Address: listener.Addr().String()})

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected the refused sender to fail the send")
    }

    if false == strings.Contains(sendErr.Error(), "smtp mail command failed") {
        t.Fatalf("expected the failure to name the command, got %v", sendErr)
    }
}

func serveThenBreakQuitSmtp(listener net.Listener) {
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

    inData := false
    for {
        line, readErr := reader.ReadString('\n')
        if nil != readErr {
            return
        }

        if true == inData {
            if "." == strings.TrimSpace(line) {
                inData = false
                writeLine("250 queued")
            }
            continue
        }

        command := strings.ToUpper(strings.TrimSpace(line))
        switch {
        case strings.HasPrefix(command, "EHLO") || strings.HasPrefix(command, "HELO"):
            writeLine("250-fake greets you")
            writeLine("250 SIZE 35882577")
        case strings.HasPrefix(command, "DATA"):
            inData = true
            writeLine("354 go ahead")
        case strings.HasPrefix(command, "QUIT"):
            return
        default:
            writeLine("250 ok")
        }
    }
}

func TestSmtpTransport_QuitFailureWarningCarriesTheCause(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveThenBreakQuitSmtp(listener)

    runtimeInstance, logger := testRuntimeWithRecordingLogger()

    transport := NewSmtpTransport(SmtpConfig{Address: listener.Addr().String()})

    sendErr := transport.Send(runtimeInstance, mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil != sendErr {
        t.Fatalf("a quit failure after acceptance must not fail the send: %v", sendErr)
    }

    warningContext, found := logger.contextOfMessage("smtp quit failed after the message was accepted")
    if false == found {
        t.Fatalf("expected the quit failure to be warned")
    }

    if _, hasCause := warningContext["error"]; false == hasCause {
        t.Fatalf("expected the quit warning to carry its cause, got context %v", warningContext)
    }
}
