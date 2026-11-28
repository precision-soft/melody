package mailer

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

/* NewLogTransport answers a transport that writes the recipients (To, Cc, Bcc), the subject, both bodies and each attachment's metadata to the logger instead of delivering the message, so a development app never sends real mail. */
func NewLogTransport(logger loggingcontract.Logger) *LogTransport {
    return &LogTransport{logger: logger}
}

type LogTransport struct {
    logger loggingcontract.Logger
}

/* Send prefers the logger supplied at construction; when it is nil the runtime's logger is resolved quietly, and with neither the send is a no-op. It builds no wire message, so it answers the zero receipt. */
func (instance *LogTransport) Send(runtimeInstance containercontract.Runtime, message mailercontract.Message) (mailercontract.Receipt, error) {
    logger := instance.logger
    if true == internal.IsNilInterface(logger) {
        resolved, _ := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
        logger = resolved
    }

    if true == internal.IsNilInterface(logger) {
        return mailercontract.Receipt{}, nil
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

    return mailercontract.Receipt{}, nil
}

/* describeAttachments summarizes each attachment as metadata only, never its content, and answers nil for a message with no attachments. */
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
