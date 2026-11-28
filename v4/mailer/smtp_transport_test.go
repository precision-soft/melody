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

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"

    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

func TestSmtpTransport_RequireAuthenticationFailsWhenServerHasNoAuthExtension(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        AllowPlaintext: true,
        Username:              "user",
        Password:              "pass",
        RequireAuthentication: true,
    })

    _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected RequireAuthentication to fail when the server does not advertise AUTH")
    }

    if false == strings.Contains(sendErr.Error(), "AUTH") {
        t.Fatalf("expected an AUTH-related error, got %v", sendErr)
    }
}

func TestSmtpTransport_RequireAuthenticationFailsWhenNoUsernameConfigured(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        AllowPlaintext: true,
        RequireAuthentication: true,
    })

    _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected RequireAuthentication to fail closed when no username is configured")
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:                  "smtp.internal.example",
        Username:              "user",
        Password:              "pass",
        RequireAuthentication: true,
        TlsConfig:             &tls.Config{InsecureSkipVerify: true},
    })

    _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
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

        /* accept, then stay silent: no 220 greeting ever arrives */
        <-time.After(5 * time.Second)
        connection.Close()
    }()

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:        "127.0.0.1",
        DialTimeout: bound.Of(150 * time.Millisecond),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

        /* accept, then stay silent well past the cancellation below: no 220 greeting ever arrives */
        <-time.After(10 * time.Second)
        connection.Close()
    }()

    /* the dial timeout is long, so only the cancellation can end this send */
    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:        "127.0.0.1",
        DialTimeout: bound.Of(10 * time.Second),
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntimeWithContext(ctx), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:    "127.0.0.1",
        Timeout: bound.Of(150 * time.Millisecond),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:    "127.0.0.1",
        Timeout: bound.Of(10 * time.Second),
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntimeWithContext(ctx), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:        "127.0.0.1",
        Username:    "user",
        Password:    "pass",
        ImplicitTls: true,
        TlsConfig:   &tls.Config{InsecureSkipVerify: true},
        Timeout:     bound.Of(200 * time.Millisecond),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{})

    _, sendErr := transport.Send(nil, mailercontract.Message{
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

    transport := NewSmtpTransport("203.0.113.10:465", SmtpConfig{
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

    transport := NewSmtpTransport("203.0.113.10:465", SmtpConfig{
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        AllowPlaintext: true,
        Host:    "127.0.0.1",
        Timeout: bound.Of(500 * time.Millisecond),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    strings.Repeat("melody carries a large body across a slow but steady link\n", 140000),
        }))
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

/* the payload must exceed the socket buffers, or the client waits on the dot acknowledgment instead */
func TestSmtpTransport_TimesOutWhenServerStopsReadingMidPayload(t *testing.T) {
    listener := listenWithSmallReceiveBuffer(t)
    defer listener.Close()

    released := make(chan struct{})
    defer close(released)

    stalled := make(chan struct{})

    go serveStallMidDataSmtp(listener, released, stalled)

    const sessionTimeout = 500 * time.Millisecond

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        AllowPlaintext: true,
        Host:    "127.0.0.1",
        Timeout: bound.Of(sessionTimeout),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    strings.Repeat("melody carries a large body across a slow but steady link\n", 140000),
        }))
    }()

    select {
    case <-stalled:
    case sendErr := <-finished:
        t.Fatalf("the send ended before the server stalled, so the stall was never exercised: %v", sendErr)
    case <-time.After(60 * time.Second):
        t.Fatal("the server never reached the stall point")
    }

    stalledAt := time.Now()

    /* buffered bytes may still flush after reading stops, so the bound is a small multiple of the timeout */
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

            /* timed from the instant reading stops: what comes before tracks machine load */
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
            config:   SmtpConfig{Timeout: bound.Of(5 * time.Second)},
            expected: 2 * time.Minute,
        },
        {
            name:     "wide per-step timeout derives four steps",
            config:   SmtpConfig{Timeout: bound.Of(40 * time.Second)},
            expected: 160 * time.Second,
        },
        {
            name:     "explicit value wins over the derivation",
            config:   SmtpConfig{Timeout: bound.Of(40 * time.Second), DataTerminationTimeout: bound.Of(90 * time.Second)},
            expected: 90 * time.Second,
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            transport := NewSmtpTransport("smtp:25", testCase.config)
            if testCase.expected != transport.dataTerminationTimeout {
                t.Fatalf("expected the dot-acknowledgment ceiling %v, got %v", testCase.expected, transport.dataTerminationTimeout)
            }
        })
    }
}

func TestNewSmtpTransport_ResolvesTheAbsentTimeouts(t *testing.T) {
    transport := NewSmtpTransport("smtp:25", SmtpConfig{})
    if 30*time.Second != transport.dialTimeout || 30*time.Second != transport.commandTimeout || 2*time.Minute != transport.dataTerminationTimeout {
        t.Fatalf("expected 30s / 30s / 2m, got %v / %v / %v", transport.dialTimeout, transport.commandTimeout, transport.dataTerminationTimeout)
    }

    derived := NewSmtpTransport("smtp:25", SmtpConfig{DialTimeout: bound.Of(45 * time.Second)})
    if 45*time.Second != derived.dialTimeout || 45*time.Second != derived.commandTimeout || 3*time.Minute != derived.dataTerminationTimeout {
        t.Fatalf("expected the dial timeout kept, the command timeout following it and the dot ceiling four of it, got %v / %v / %v", derived.dialTimeout, derived.commandTimeout, derived.dataTerminationTimeout)
    }
}

func TestNewSmtpTransport_RefusesAnAddressThatIsEmptyOrNotHostAndPort(t *testing.T) {
    for address, message := range map[string]string{
        "":                 "smtp address is empty",
        "smtp":             "smtp address is not host:port",
        "smtp.example.com": "smtp address is not host:port",
        "smtp:":            "smtp address is not host:port",
        "[::1":             "smtp address is not host:port",
        ":25":              "smtp address is not host:port",
        " smtp:25":         "smtp address is not host:port",
        "smtp:25 ":         "smtp address is not host:port",
        "smtp:abc":         "smtp address names a port the dial cannot read",
        "smtp:70000":       "smtp address names a port the dial cannot read",
    } {
        func() {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || message != refusal.Message() || "address" != refusal.Context()["argument"] {
                    t.Fatalf("%q: expected %q naming the address argument, got %v", address, message, refusal)
                }
            }()

            NewSmtpTransport(address, SmtpConfig{DialTimeout: bound.Of(time.Microsecond)})
        }()
    }

    for _, address := range []string{"smtp:25", "smtp.example.com:587", "[::1]:25"} {
        NewSmtpTransport(address, SmtpConfig{})
    }
}

func TestNewSmtpTransport_RefusesATimeoutOutsideItsRuleByName(t *testing.T) {
    cases := map[string]struct {
        config  SmtpConfig
        message string
        field   string
    }{
        "dial timeout below 1ms":             {SmtpConfig{DialTimeout: bound.Of(time.Microsecond)}, "the limit is below its minimum", "SmtpConfig.DialTimeout"},
        "dial timeout unlimited":             {SmtpConfig{DialTimeout: bound.UnlimitedDuration()}, "the limit does not admit unlimited", "SmtpConfig.DialTimeout"},
        "timeout below 1ms":                  {SmtpConfig{Timeout: bound.Of(time.Microsecond)}, "the limit is below its minimum", "SmtpConfig.Timeout"},
        "timeout negative":                   {SmtpConfig{Timeout: bound.Of(-time.Second)}, "the limit is below its minimum", "SmtpConfig.Timeout"},
        "timeout unlimited":                  {SmtpConfig{Timeout: bound.UnlimitedDuration()}, "the limit does not admit unlimited", "SmtpConfig.Timeout"},
        "data termination timeout below 1ms": {SmtpConfig{DataTerminationTimeout: bound.Of(time.Microsecond)}, "the limit is below its minimum", "SmtpConfig.DataTerminationTimeout"},
        "data termination timeout unlimited": {SmtpConfig{DataTerminationTimeout: bound.UnlimitedDuration()}, "the limit does not admit unlimited", "SmtpConfig.DataTerminationTimeout"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                NewSmtpTransport("smtp:25", testCase.config)
            }, testCase.message)

            var context map[string]any
            func() {
                defer func() {
                    if exceptionErr, isExceptionError := recover().(*exception.Error); true == isExceptionError && nil != exceptionErr {
                        context = exceptionErr.Context()
                    }
                }()

                NewSmtpTransport("smtp:25", testCase.config)
            }()
            if testCase.field != context["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", testCase.field, context)
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        AllowPlaintext: true,
        Host:                   "127.0.0.1",
        Timeout:                bound.Of(200 * time.Millisecond),
        DataTerminationTimeout: bound.Of(5 * time.Second),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

    /* TEST-NET-3 is reserved and unroutable, so an uncancelled connect would hang until the dial timeout */
    transport := NewSmtpTransport("203.0.113.1:25", SmtpConfig{
        Host:        "203.0.113.1",
        DialTimeout: bound.Of(5 * time.Second),
    })

    finished := make(chan error, 1)
    go func() {
        finished <- sendErrorOf(transport.Send(testRuntimeWithContext(ctx), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        }))
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

    /* RequireAuthentication deliberately left false, its zero value */
    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        AllowPlaintext: true,
        Username: "user",
        Password: "pass",
    })

    _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
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
    transport := NewSmtpTransport("127.0.0.1:0", SmtpConfig{})

    var typedNil *nilRuntime

    _, sendErr := transport.Send(typedNil, mailercontract.Message{
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{AllowPlaintext: true})

    _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
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

func serveRcptRejectingSmtp(listener net.Listener) {
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
        case strings.HasPrefix(command, "RCPT"):
            writeLine("550 recipient refused")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
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

    transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{AllowPlaintext: true})

    _, sendErr := transport.Send(runtimeInstance, mailercontract.Message{
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

func serveDotRejectingSmtp(listener net.Listener) {
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
                writeLine("554 message rejected at the dot")
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
            writeLine("354 end data with <CR><LF>.<CR><LF>")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}

func TestSmtpTransport_EnvelopeRefusalsCarryTheAddressAndTheDotVerdictIsNamed(t *testing.T) {
    t.Run("mail carries the address", func(t *testing.T) {
        listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
        if nil != listenErr {
            t.Fatalf("listen: %v", listenErr)
        }
        defer listener.Close()

        go serveMailRejectingSmtp(listener)

        transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{AllowPlaintext: true})
        _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })

        var melodyErr *exception.Error
        if false == errors.As(sendErr, &melodyErr) || "smtp mail command failed" != melodyErr.Message() {
            t.Fatalf("expected the mail refusal, got %v", sendErr)
        }

        if listener.Addr().String() != melodyErr.Context()["address"] {
            t.Fatalf("expected the refusal to carry the relay's address, got %v", melodyErr.Context())
        }
    })

    t.Run("rcpt carries the address", func(t *testing.T) {
        listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
        if nil != listenErr {
            t.Fatalf("listen: %v", listenErr)
        }
        defer listener.Close()

        go serveRcptRejectingSmtp(listener)

        transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{AllowPlaintext: true})
        _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })

        var melodyErr *exception.Error
        if false == errors.As(sendErr, &melodyErr) || "smtp rcpt command failed" != melodyErr.Message() {
            t.Fatalf("expected the rcpt refusal, got %v", sendErr)
        }

        if listener.Addr().String() != melodyErr.Context()["address"] || "ada@example.com" != melodyErr.Context()["recipient"] {
            t.Fatalf("expected the refusal to carry the relay's address and the recipient, got %v", melodyErr.Context())
        }
    })

    t.Run("the dot verdict is named", func(t *testing.T) {
        listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
        if nil != listenErr {
            t.Fatalf("listen: %v", listenErr)
        }
        defer listener.Close()

        go serveDotRejectingSmtp(listener)

        transport := NewSmtpTransport(listener.Addr().String(), SmtpConfig{AllowPlaintext: true})
        _, sendErr := transport.Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
        })

        var melodyErr *exception.Error
        if false == errors.As(sendErr, &melodyErr) || "smtp dot acknowledgment failed" != melodyErr.Message() {
            t.Fatalf("expected the dot verdict to be named, got %v", sendErr)
        }
    })
}

func TestNewSmtpTransport_LocalNameDefaultsToLocalhostAndRefusesWhitespace(t *testing.T) {
    if "localhost" != NewSmtpTransport("smtp:25", SmtpConfig{}).localName {
        t.Fatal("expected the hello name to default to localhost")
    }

    for _, localName := range []string{"mail example", "mail\r\nMAIL FROM:<a@b>", " "} {
        testhelper.AssertPanicsWithError(t, func() {
            NewSmtpTransport("smtp:25", SmtpConfig{LocalName: localName})
        }, "smtp local name holds whitespace or a control character")
    }
}

func TestSmtpTransport_SendsTheConfiguredLocalNameInTheHello(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    hello := make(chan string, 1)
    go func() {
        connection, acceptErr := listener.Accept()
        if nil != acceptErr {
            return
        }
        defer connection.Close()

        reader := bufio.NewReader(connection)
        _, _ = connection.Write([]byte("220 fake ESMTP\r\n"))

        line, _ := reader.ReadString('\n')
        hello <- strings.TrimSpace(line)
    }()

    _, _ = NewSmtpTransport(listener.Addr().String(), SmtpConfig{LocalName: "mail.example.com", Timeout: bound.Of(time.Second)}).Send(testRuntime(), mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Text: "body",
    })

    select {
    case line := <-hello:
        if "EHLO mail.example.com" != line {
            t.Fatalf("expected the configured name in the hello, got %q", line)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the server saw no hello")
    }
}

func TestSmtpTransport_RefusesAnAddressThatIsNotABareAddrSpecBeforeTheDial(t *testing.T) {
    _, sendErr := NewSmtpTransport("127.0.0.1:1", SmtpConfig{}).Send(testRuntime(), mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "a@x.com>, <b@y.com"}},
        Text: "body",
    })

    if nil == sendErr || false == strings.Contains(sendErr.Error(), "mailer message") {
        t.Fatalf("expected the address refused by the transport itself, got %v", sendErr)
    }
}

func serveCapturingSmtp(listener net.Listener, captured chan<- string) {
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
            writeLine("250 fake greets you")
        case strings.HasPrefix(command, "DATA"):
            writeLine("354 end with .")

            body := strings.Builder{}
            for {
                dataLine, dataErr := reader.ReadString('\n')
                if nil != dataErr {
                    return
                }
                if ".\r\n" == dataLine {
                    break
                }
                body.WriteString(dataLine)
            }

            captured <- body.String()
            writeLine("250 queued")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}

func TestSmtpTransport_TheReceiptCarriesTheMessageIdTheServerReceived(t *testing.T) {
    for name, headers := range map[string]map[string]string{
        "minted":       nil,
        "caller's own": {"Message-ID": " <own@example.com> "},
    } {
        listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
        if nil != listenErr {
            t.Fatalf("listen: %v", listenErr)
        }

        captured := make(chan string, 1)
        go serveCapturingSmtp(listener, captured)

        receipt, sendErr := NewSmtpTransport(listener.Addr().String(), SmtpConfig{AllowPlaintext: true, Host: "127.0.0.1", Timeout: bound.Of(2 * time.Second)}).Send(testRuntime(), mailercontract.Message{
            From:    mailercontract.Address{Email: "shop@example.com"},
            To:      []mailercontract.Address{{Email: "ada@example.com"}},
            Subject: "Hello",
            Text:    "body",
            Headers: headers,
        })
        listener.Close()
        if nil != sendErr {
            t.Fatalf("%s: unexpected send error: %v", name, sendErr)
        }

        body := <-captured
        if "" == receipt.MessageId || false == strings.Contains(body, "Message-ID: "+receipt.MessageId+"\r\n") {
            t.Fatalf("%s: expected the receipt's id %q as the Message-ID the server received, got:\n%s", name, receipt.MessageId, body)
        }

        if "caller's own" == name && "<own@example.com>" != receipt.MessageId {
            t.Fatalf("expected the caller's own Message-ID, got %q", receipt.MessageId)
        }
    }
}

func TestSmtpTransport_ZeroConfigRefusesARelayWithoutStartTls(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    captured := make(chan string, 1)
    go serveCapturingSmtp(listener, captured)

    _, sendErr := NewSmtpTransport(listener.Addr().String(), SmtpConfig{Host: "127.0.0.1", Timeout: bound.Of(2 * time.Second)}).Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })

    var refusal *exception.Error
    if false == errors.As(sendErr, &refusal) || "smtp server does not offer STARTTLS but tls is required" != refusal.Message() || "SmtpConfig.AllowPlaintext" != refusal.Context()["door"] {
        t.Fatalf("expected the default to refuse a relay without STARTTLS naming the door, got %v", sendErr)
    }

    select {
    case body := <-captured:
        t.Fatalf("expected nothing sent, the server received:\n%s", body)
    default:
    }
}

func TestSmtpTransport_AllowPlaintextSendsToARelayWithoutStartTls(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    captured := make(chan string, 1)
    go serveCapturingSmtp(listener, captured)

    _, sendErr := NewSmtpTransport(listener.Addr().String(), SmtpConfig{Host: "127.0.0.1", AllowPlaintext: true, Timeout: bound.Of(2 * time.Second)}).Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil != sendErr {
        t.Fatalf("expected the plaintext door to deliver, got %v", sendErr)
    }

    if body := <-captured; false == strings.Contains(body, "Subject: Hello") {
        t.Fatalf("expected the message delivered, the server received:\n%s", body)
    }
}

func TestSmtpTransport_AllowPlaintextStillUpgradesWhenStartTlsIsOffered(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    authResult := make(chan bool, 1)
    go serveStartTlsAuthSmtp(listener, generateSelfSignedCertificate(t), authResult)

    /* smtp.PlainAuth refuses this host without tls, so an accepted AUTH proves the upgrade */
    _, sendErr := NewSmtpTransport(listener.Addr().String(), SmtpConfig{
        Host:                  "smtp.internal.example",
        Username:              "user",
        Password:              "pass",
        RequireAuthentication: true,
        AllowPlaintext:        true,
        TlsConfig:             &tls.Config{InsecureSkipVerify: true},
    }).Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil != sendErr {
        t.Fatalf("expected the offered STARTTLS taken under the plaintext door, got %v", sendErr)
    }

    if true != <-authResult {
        t.Fatal("expected the server to accept AUTH over the upgraded session")
    }
}

func TestNewSmtpTransport_RefusesAllowPlaintextUnderImplicitTls(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "smtp plaintext is allowed under implicit tls, which never sends in clear" != refusal.Message() || "SmtpConfig.AllowPlaintext" != refusal.Context()["field"] || "SmtpConfig.ImplicitTls" != refusal.Context()["conflictsWith"] {
            t.Fatalf("expected the plaintext door refused under implicit tls naming both fields, got %v", refusal)
        }
    }()

    NewSmtpTransport("smtp.example.com:465", SmtpConfig{AllowPlaintext: true, ImplicitTls: true})
}
