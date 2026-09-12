package logging

import (
    "log"
    "strings"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* NewStandardErrorLogger adapts a melody logger to the *log.Logger net/http's Server.ErrorLog wants, so the connection-level failures the http kernel never sees reach the application's journal instead of the standard logger's default output. A tls handshake that fails before any request exists, a malformed request line, a listener degrading while the process stays up, a superfluous WriteHeader, and the one panic no request guard can absorb — the one raised while the request scope is closing — all arrive here. Written to stderr as unstructured text they are invisible to a deployment whose journal is a json file.

   The flags are zero: the record carries the logger's own timestamp, and the standard prefix would only repeat it inside the message. */
func NewStandardErrorLogger(logger loggingcontract.Logger, message string) *log.Logger {
    return log.New(&standardLogWriter{logger: logger, message: message}, "", 0)
}

type standardLogWriter struct {
    logger  loggingcontract.Logger
    message string
}

/* Write files one record per line the standard logger emits. The line travels in the context rather than in the message, so the message stays the one groupable string an operator can query on. These are the routine noise of an internet-facing listener — a scanner, an expired client certificate — and are recorded at warning, the level the write path already gives a client that walked away mid-response. */
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
