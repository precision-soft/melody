package mailer

import (
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* @info LogTransport writes the recipients (To, Cc, Bcc), subject, both the text and HTML bodies, and per-attachment metadata of every message to the logger instead of delivering it; intended for local development so a misconfigured app never sends real mail */
func NewLogTransport(logger loggingcontract.Logger) *LogTransport {
    return &LogTransport{logger: logger}
}

type LogTransport struct {
    logger loggingcontract.Logger
}

/* @info the logger supplied at construction is preferred; when it is nil the request-scoped logger is resolved quietly from the runtime (a missing logger service is swallowed rather than emitting an emergency log on every send), and when neither is available the send is a safe no-op */
func (instance *LogTransport) Send(runtimeInstance runtimecontract.Runtime, message mailercontract.Message) error {
    logger := instance.logger
    if nil == logger {
        resolved, _ := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
        logger = resolved
    }

    if nil == logger {
        return nil
    }

    logger.Info(
        "mailer log transport captured a message",
        loggingcontract.Context{
            "to":          appendEmails(nil, message.To),
            "cc":          appendEmails(nil, message.Cc),
            "bcc":         appendEmails(nil, message.Bcc),
            "subject":     message.Subject,
            "text":        message.Text,
            "html":        message.Html,
            "attachments": describeAttachments(message.Attachments),
        },
    )

    return nil
}

/* @info summarizes each attachment as metadata only (filename, content type, Content-ID, inline flag, byte size) — never the raw content — so an inline image embedded for an HTML body is visible in the dev log without dumping its bytes; nil when the message carries no attachments, mirroring appendEmails */
func describeAttachments(attachments []mailercontract.Attachment) []map[string]any {
    if 0 == len(attachments) {
        return nil
    }

    summaries := make([]map[string]any, 0, len(attachments))
    for _, attachment := range attachments {
        summaries = append(summaries, map[string]any{
            "filename":    attachment.Filename,
            "contentType": attachment.ContentType,
            "contentId":   attachment.ContentId,
            "inline":      "" != attachment.ContentId,
            "bytes":       len(attachment.Content),
        })
    }

    return summaries
}

var _ mailercontract.Transport = (*LogTransport)(nil)
