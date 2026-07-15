package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    melodymailer "github.com/precision-soft/melody/v3/mailer"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

/* runMailCheck sends a message through melody's SmtpTransport to a real smtp server (mailpit) and confirms
it arrives, exercising the whole session — auth-less greeting, MAIL/RCPT/DATA, the payload write and QUIT —
under the per-step session deadline. A configured Timeout bounds every step past the greeting, so a stalled
relay can no longer pin the sender; here the server is healthy and the message must land intact. */
func runMailCheck(smtpAddress string, mailpitApiUrl string) {
    subject := fmt.Sprintf("melody-e2e-%d", time.Now().UnixNano())

    transport := melodymailer.NewSmtpTransport(melodymailer.SmtpConfig{
        Address: smtpAddress,
        Timeout: 5 * time.Second,
    })

    message := mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com", Name: "Melody Shop"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: subject,
        Text:    "Hello from the melody e2e harness",
        Html:    "<h1>Hello</h1><p>from the melody e2e harness</p>",
    }

    if sendErr := transport.Send(newRuntime(), message); nil != sendErr {
        fail("mail send through the smtp transport failed: %v", sendErr)
    }

    pass("sent a message through the smtp transport to %s under a per-step session deadline", smtpAddress)

    if false == waitForMailpitMessage(mailpitApiUrl, subject) {
        fail("mailpit never received the message with subject %q at %s", subject, mailpitApiUrl)
    }

    pass("mailpit received the message with subject %q", subject)
}

func waitForMailpitMessage(mailpitApiUrl string, subject string) bool {
    deadline := time.Now().Add(10 * time.Second)

    for time.Now().Before(deadline) {
        if true == mailpitHasSubject(mailpitApiUrl, subject) {
            return true
        }

        time.Sleep(250 * time.Millisecond)
    }

    return false
}

func mailpitHasSubject(mailpitApiUrl string, subject string) bool {
    endpoint := strings.TrimRight(mailpitApiUrl, "/") + "/api/v1/messages"

    response, getErr := http.Get(endpoint)
    if nil != getErr {
        return false
    }
    defer response.Body.Close()

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        return false
    }

    var payload struct {
        Messages []struct {
            Subject string `json:"Subject"`
        } `json:"messages"`
    }

    if unmarshalErr := json.Unmarshal(body, &payload); nil != unmarshalErr {
        return false
    }

    for _, mailpitMessage := range payload.Messages {
        if subject == mailpitMessage.Subject {
            return true
        }
    }

    return false
}
