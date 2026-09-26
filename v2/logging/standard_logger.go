package logging

import (
    "log"
    "strings"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

/* NewStandardErrorLogger adapts a melody logger to the *log.Logger net/http's Server.ErrorLog wants, so the connection-level failures the kernel never sees reach the journal: a failed tls handshake, a malformed request line, a superfluous WriteHeader, a panic while the request scope closes. The flags are zero, since the record carries its own timestamp. */
func NewStandardErrorLogger(logger loggingcontract.Logger, message string) *log.Logger {
    return log.New(&standardLogWriter{logger: logger, message: message}, "", 0)
}

type standardLogWriter struct {
    logger  loggingcontract.Logger
    message string
}

/* Write files one record per line the standard logger emits, at warning. The line travels in the context, so the message stays one groupable string. */
func (instance *standardLogWriter) Write(data []byte) (int, error) {
    written := len(data)

    line := strings.TrimRight(string(data), "\n")
    if "" == line {
        return written, nil
    }

    if nil == instance.logger {
        return written, nil
    }

    instance.logger.Warning(
        instance.message,
        loggingcontract.Context{
            "line": line,
        },
    )

    return written, nil
}
