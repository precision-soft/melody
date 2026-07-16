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

    if sendErr := sendWithRetry(transport, message); nil != sendErr {
        fail("mail send through the smtp transport failed: %v", sendErr)
    }

    pass("sent a message through the smtp transport to %s under a per-step session deadline", smtpAddress)

    received, lastPollErr := waitForMailpitMessage(mailpitApiUrl, subject)
    if false == received {
        if nil != lastPollErr {
            fail("mailpit never received the message with subject %q at %s (last poll error: %v)", subject, mailpitApiUrl, lastPollErr)
        }

        fail("mailpit never received the message with subject %q at %s", subject, mailpitApiUrl)
    }

    pass("mailpit received the message with subject %q", subject)
}

/* sendWithRetry absorbs the short window where mailpit accepted the container start but is not yet
listening; a persistent failure still surfaces after the attempts are exhausted. */
func sendWithRetry(transport mailercontract.Transport, message mailercontract.Message) error {
    var sendErr error

    for attempt := 0; attempt < 5; attempt++ {
        if 0 < attempt {
            time.Sleep(time.Second)
        }

        sendErr = transport.Send(newRuntime(), message)
        if nil == sendErr {
            return nil
        }
    }

    return sendErr
}

/* waitForMailpitMessage reports whether the subject showed up before the deadline; the last poll error is
returned so an unreachable or broken mailpit api is distinguishable from a message that never arrived. */
func waitForMailpitMessage(mailpitApiUrl string, subject string) (bool, error) {
    deadline := time.Now().Add(10 * time.Second)

    var lastPollErr error

    for time.Now().Before(deadline) {
        found, pollErr := mailpitHasSubject(mailpitApiUrl, subject)
        if true == found {
            return true, nil
        }
        if nil != pollErr {
            lastPollErr = pollErr
        }

        time.Sleep(250 * time.Millisecond)
    }

    return false, lastPollErr
}

func mailpitHasSubject(mailpitApiUrl string, subject string) (bool, error) {
    endpoint := strings.TrimRight(mailpitApiUrl, "/") + "/api/v1/messages"

    response, getErr := http.Get(endpoint)
    if nil != getErr {
        return false, getErr
    }
    defer response.Body.Close()

    if http.StatusOK != response.StatusCode {
        return false, fmt.Errorf("mailpit api responded with status %d", response.StatusCode)
    }

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        return false, readErr
    }

    var payload struct {
        Messages []struct {
            Subject string `json:"Subject"`
        } `json:"messages"`
    }

    if unmarshalErr := json.Unmarshal(body, &payload); nil != unmarshalErr {
        return false, unmarshalErr
    }

    for _, mailpitMessage := range payload.Messages {
        if subject == mailpitMessage.Subject {
            return true, nil
        }
    }

    return false, nil
}
