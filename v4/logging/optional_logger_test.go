package logging_test

import (
    "errors"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/http/static"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/messagebus"
    "github.com/precision-soft/melody/v4/session"
)

type typedNilLogger struct {
    loggingcontract.Logger
}

func TestResolveOptionalLogger_AHandedLoggerIsKept(t *testing.T) {
    handed := logging.NewNopLogger()

    resolved, resolveErr := logging.ResolveOptionalLogger(handed, "component", "Config.Logger")
    if nil != resolveErr || handed != resolved {
        t.Fatalf("expected the handed logger, got %v / %v", resolved, resolveErr)
    }
}

func TestResolveOptionalLogger_ATypedNilIsRefusedNamingTheField(t *testing.T) {
    var typedNil *typedNilLogger

    resolved, resolveErr := logging.ResolveOptionalLogger(typedNil, "component", "Config.Logger")
    if nil != resolved {
        t.Fatalf("expected no logger, got %v", resolved)
    }

    requireTypedNilLoggerRefusal(t, resolveErr, "component logger is a typed nil", "Config.Logger")
}

func TestResolveOptionalLogger_NilIsTheEmergencyLoggerEvenAfterItsClose(t *testing.T) {
    first, firstErr := logging.ResolveOptionalLogger(nil, "component", "Config.Logger")
    second, secondErr := logging.ResolveOptionalLogger(nil, "other", "Other.Logger")
    if nil != firstErr || nil != secondErr || nil == first || first != second {
        t.Fatalf("expected one emergency default, got %v / %v", first, second)
    }

    logging.CloseEmergencyLogger()

    written := captureStandardError(t, func() {
        first.Error("a failure nobody else would see", nil)
    })

    if false == strings.Contains(written, "a failure nobody else would see") {
        t.Fatalf("expected the record on the emergency logger, got %q", written)
    }

    if false == first.Enabled(loggingcontract.LevelError) || 0 >= first.CauseChainDepth() {
        t.Fatalf("expected the default to answer as the emergency logger")
    }
}

func TestOptionalDependencies_NilIsTheDefaultAndATypedNilIsRefused(t *testing.T) {
    var typedNil *typedNilLogger

    rows := []struct {
        name    string
        message string
        field   string
        absentRecord string
        build        func(logger loggingcontract.Logger) error
    }{
        {"http.NewServerSentEventHub", "server sent event hub logger is a typed nil", "ServerSentEventHubConfig.Logger", "", func(logger loggingcontract.Logger) error {
            return capturePanic(func() { http.NewServerSentEventHub(http.ServerSentEventHubConfig{Logger: logger}) })
        }},
        {"messagebus.NewInMemoryTransport", "in-memory transport logger is a typed nil", "InMemoryTransportConfig.Logger", "", func(logger loggingcontract.Logger) error {
            return capturePanic(func() { messagebus.NewInMemoryTransport(messagebus.InMemoryTransportConfig{Logger: logger}) })
        }},
        {"session.NewFileStorage", "session file storage logger is a typed nil", "FileStorageConfig.Logger", "", func(logger loggingcontract.Logger) error {
            storage, storageErr := session.NewFileStorage(filepath.Join(t.TempDir(), "sessions"), session.FileStorageConfig{Logger: logger})
            if nil != storage {
                _ = storage.CloseWithContext(t.Context())
            }

            return storageErr
        }},
        {"static.FileServer.Serve", "static file server logger is a typed nil", "ServeOptions.Logger", "static serve skipped because request is nil", func(logger loggingcontract.Logger) error {
            return capturePanic(func() { static.NewFileServer(static.Config{Root: t.TempDir()}).Serve(nil, static.ServeOptions{Logger: logger}) })
        }},
        {"static.FileServer.ServeReader", "static file server logger is a typed nil", "ServeOptions.Logger", "static serve reader skipped because request is nil", func(logger loggingcontract.Logger) error {
            return capturePanic(func() { static.NewFileServer(static.Config{Root: t.TempDir()}).ServeReader(nil, static.ServeOptions{Logger: logger}) })
        }},
        {"http.CloseDiscardedResponseBody", "discarded response body logger is a typed nil", "DiscardedBodyOptions.Logger", "failed to close discarded response body", func(logger loggingcontract.Logger) error {
            response := http.TextResponse(200, "discarded")
            response.SetBodyReader(failingCloseReader{Reader: strings.NewReader("discarded")})

            return capturePanic(func() { http.CloseDiscardedResponseBody(response, http.DiscardedBodyOptions{Logger: logger}) })
        }},
    }

    for _, row := range rows {
        t.Run(row.name, func(t *testing.T) {
            var buildErr error
            written := captureStandardError(t, func() { buildErr = row.build(nil) })
            if nil != buildErr {
                t.Fatalf("expected an absent logger to build, got %v", buildErr)
            }

            if "" != row.absentRecord && false == strings.Contains(written, row.absentRecord) {
                t.Fatalf("expected an absent logger to be the emergency logger, got %q", written)
            }

            requireTypedNilLoggerRefusal(t, row.build(typedNil), row.message, row.field)
        })
    }
}

type failingCloseReader struct {
    io.Reader
}

func (instance failingCloseReader) Close() error {
    return errors.New("close failed")
}

func captureStandardError(t *testing.T, action func()) string {
    t.Helper()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }

    savedStandardError := os.Stderr
    logging.CloseEmergencyLogger()
    os.Stderr = writeEnd
    _ = logging.EmergencyLogger()
    os.Stderr = savedStandardError

    action()

    logging.CloseEmergencyLogger()
    _ = writeEnd.Close()

    written, readErr := io.ReadAll(readEnd)
    _ = readEnd.Close()
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    return string(written)
}

func capturePanic(callback func()) (recoveredErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            if panicErr, isError := recovered.(error); true == isError {
                recoveredErr = panicErr

                return
            }

            recoveredErr = errors.New("panicked with a value that is not an error")
        }
    }()

    callback()

    return nil
}

func requireTypedNilLoggerRefusal(t *testing.T, refusalErr error, message string, field string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(refusalErr, &refusal) {
        t.Fatalf("expected the refusal %q, got %v", message, refusalErr)
    }

    if message != refusal.Message() {
        t.Fatalf("expected the message %q, got %q", message, refusal.Message())
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the field %q, got %v", field, refusal.Context()["field"])
    }
}
