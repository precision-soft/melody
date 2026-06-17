package mailer

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

type capturedLog struct {
    level   loggingcontract.Level
    message string
    context loggingcontract.Context
}

type capturingLogger struct {
    entries []capturedLog
}

func (instance *capturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.entries = append(instance.entries, capturedLog{level: level, message: message, context: context})
}

func (instance *capturingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *capturingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *capturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *capturingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *capturingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func TestLogTransportLogsRecipientsSubjectAndText(t *testing.T) {
    logger := &capturingLogger{}
    transport := NewLogTransport(logger)

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Cc:      []mailercontract.Address{{Email: "ops@example.com"}},
        Subject: "Welcome",
        Text:    "hello there",
        Html:    "<p>hello there</p>",
    })
    if nil != sendErr {
        t.Fatalf("Send returned unexpected error: %v", sendErr)
    }

    if 1 != len(logger.entries) {
        t.Fatalf("expected exactly one log entry, got %d", len(logger.entries))
    }

    entry := logger.entries[0]
    if loggingcontract.LevelInfo != entry.level {
        t.Fatalf("expected the message to be logged at info level, got %v", entry.level)
    }

    if "Welcome" != entry.context["subject"] {
        t.Fatalf("expected subject in the log context, got %v", entry.context["subject"])
    }

    if "hello there" != entry.context["text"] {
        t.Fatalf("expected text body in the log context, got %v", entry.context["text"])
    }

    if "<p>hello there</p>" != entry.context["html"] {
        t.Fatalf("expected html body in the log context, got %v", entry.context["html"])
    }

    to, ok := entry.context["to"].([]string)
    if false == ok || 1 != len(to) || "ada@example.com" != to[0] {
        t.Fatalf("expected the To recipients in the log context, got %v", entry.context["to"])
    }

    cc, ok := entry.context["cc"].([]string)
    if false == ok || 1 != len(cc) || "ops@example.com" != cc[0] {
        t.Fatalf("expected the Cc recipients in the log context, got %v", entry.context["cc"])
    }
}

func TestLogTransportSummarizesAttachments(t *testing.T) {
    logger := &capturingLogger{}
    transport := NewLogTransport(logger)

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Welcome",
        Html:    "<p><img src=\"cid:logo\"></p>",
        Attachments: []mailercontract.Attachment{
            {
                Filename:    "logo.png",
                ContentType: "image/png",
                Content:     []byte{0x01, 0x02, 0x03, 0x04},
                ContentId:   "logo",
            },
        },
    })
    if nil != sendErr {
        t.Fatalf("Send returned unexpected error: %v", sendErr)
    }

    if 1 != len(logger.entries) {
        t.Fatalf("expected exactly one log entry, got %d", len(logger.entries))
    }

    attachments, ok := logger.entries[0].context["attachments"].([]map[string]any)
    if false == ok || 1 != len(attachments) {
        t.Fatalf("expected one attachment summary in the log context, got %v", logger.entries[0].context["attachments"])
    }

    summary := attachments[0]
    if "logo.png" != summary["filename"] {
        t.Fatalf("expected the attachment filename in the summary, got %v", summary["filename"])
    }

    if "image/png" != summary["contentType"] {
        t.Fatalf("expected the attachment content type in the summary, got %v", summary["contentType"])
    }

    if "logo" != summary["contentId"] {
        t.Fatalf("expected the attachment Content-ID in the summary, got %v", summary["contentId"])
    }

    if true != summary["inline"] {
        t.Fatalf("expected the attachment to be marked inline, got %v", summary["inline"])
    }

    if 4 != summary["bytes"] {
        t.Fatalf("expected the attachment byte size in the summary, got %v", summary["bytes"])
    }
}

func TestLogTransportOmitsAttachmentsWhenNone(t *testing.T) {
    logger := &capturingLogger{}
    transport := NewLogTransport(logger)

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Welcome",
        Text:    "hello there",
    })
    if nil != sendErr {
        t.Fatalf("Send returned unexpected error: %v", sendErr)
    }

    attachments, ok := logger.entries[0].context["attachments"].([]map[string]any)
    if false == ok || 0 != len(attachments) {
        t.Fatalf("expected no attachment summaries for an attachment-free message, got %v", logger.entries[0].context["attachments"])
    }
}

func TestLogTransportIsNoOpWithoutLogger(t *testing.T) {
    transport := NewLogTransport(nil)

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Welcome",
    })
    if nil != sendErr {
        t.Fatalf("Send must be a safe no-op when no logger is available, got: %v", sendErr)
    }
}
