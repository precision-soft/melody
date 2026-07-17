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

/** @info the greeting is read after the connect, so only the cancellation watcher can unblock it — the connect's own context-awareness is already spent. A relay that accepts the connection and then says nothing must not pin the sender until the dial timeout when the runtime is cancelled. */
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

/** @info on the implicit-tls path the client sends its initial EHLO lazily on its first operation, after the greeting deadline has been cleared; the per-step session deadline must bound that hello too, or a server that greets and then goes silent pins the sending goroutine forever. */
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

/* serveImplicitTlsGreetThenStallSmtp completes the tls handshake, sends the 220 greeting and then never answers the client's EHLO, until released is closed or a safety timeout elapses — modelling an implicit-tls relay that accepts the session and immediately black-holes it. */
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

/** @info the runtime context drives mid-session cancellation, so a nil runtime must surface as an error from Send instead of reaching the cancellation watcher. */
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

/** @info a user tls config that sets neither ServerName nor InsecureSkipVerify would fail the STARTTLS handshake ("either ServerName or InsecureSkipVerify must be specified"), so the transport must fill in its host on a clone while leaving the caller's config — which may be shared — untouched. */
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

/** @info a user tls config with InsecureSkipVerify already set is complete for the handshake, so it is returned verbatim without cloning. */
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

/** @info a single absolute deadline over the whole DATA payload conflates "slow" with "stalled": a large message on a slow-but-alive link is killed once the total transfer time exceeds the session timeout even though bytes keep flowing, so the payload write must re-arm the deadline per chunk of progress instead of once for the entire body. */
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

/** @info re-arming the deadline per payload chunk must not turn it into a moving target that never fires: a peer that stops reading mid-body makes no progress, so the blocked chunk write still hits the per-step deadline and the session is cut within one timeout. */
func TestSmtpTransport_TimesOutWhenServerStopsReadingMidPayload(t *testing.T) {
    listener := listenWithSmallReceiveBuffer(t)
    defer listener.Close()

    released := make(chan struct{})
    defer close(released)

    go serveStallMidDataSmtp(listener, released)

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
        if nil == sendErr {
            t.Fatal("expected a timeout error when the server stops reading mid-payload")
        }

        var netErr net.Error
        if false == errors.As(sendErr, &netErr) || false == netErr.Timeout() {
            t.Fatalf("expected an i/o timeout error, got %v", sendErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("send was not cut within one session timeout on a server that stopped reading mid-payload")
    }
}

/* listenWithSmallReceiveBuffer opens a localhost listener whose receive buffer is clamped before the handshake, so accepted connections advertise a small window: the client's writes are then genuinely paced by the server's reads instead of vanishing into kernel buffering, which is what lets these tests observe a blocking payload write. The clamp stays above the point where the window drops so far below the loopback segment size that delayed acknowledgements throttle even a steadily-drained connection. */
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

/* serveSlowSteadyDataSmtp greets and accepts the envelope, then drains the first stretch of the DATA body in small reads separated by short pauses — each read arrives well within a per-chunk deadline while that stretch alone takes far longer than the session timeout — modelling a slow-but-alive link. The remainder is drained at full speed so the closing dot and its 250 reply, which run under a single per-step deadline, are not starved by the backlog the kernel buffered on the client's side. */
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

/* serveStallMidDataSmtp greets and accepts the envelope, drains the first stretch of the DATA body and then stops reading entirely — until released is closed or a safety timeout elapses — modelling a peer that goes dead mid-transfer. */
func serveStallMidDataSmtp(listener net.Listener, released <-chan struct{}) {
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

/** @info the dot-acknowledgment ceiling derives from the per-step timeout with a floor, so a default-configured transport leaves a scanning relay a realistic acceptance window; an explicit value always wins. */
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

/** @info a relay that runs content inspection delays the dot acknowledgment far beyond any other reply; the per-step timeout must not cut that step, or a message the server may already have queued is reported as a failure and retried into a duplicate. */
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

/* serveDelayedDotAcknowledgmentSmtp answers every step promptly and delays only the 250 after the message-ending dot, modelling a relay that runs its content inspection before accepting. */
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

/** @info the cancellation watcher only covers the running session, so the dial itself must honor the runtime context — a shutdown during a connect to an unresponsive relay must not stall for the full dial timeout. */
func TestSmtpTransport_CancelledContextAbortsDial(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    /* TEST-NET-3 is reserved and unroutable, so an uncancelled connect would hang until the dial timeout */
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
