package logging

import (
    "encoding/json"
    "io"
    "os"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func NewJsonLogger(output io.Writer, minLevel loggingcontract.Level) loggingcontract.Logger {
    return NewJsonLoggerWithLabels(output, minLevel, loggingcontract.DefaultLevelLabels())
}

func NewJsonLoggerWithLabels(output io.Writer, minLevel loggingcontract.Level, labels loggingcontract.LevelLabels) loggingcontract.Logger {
    if true == internal.IsNilInterface(output) {
        exception.Panic(
            exception.NewError("json logger output is not provided", nil, nil),
        )
    }

    if false == IsValidLevel(minLevel) {
        exception.Panic(
            exception.NewError(
                "invalid json logger min level",
                map[string]any{
                    "level": string(minLevel),
                },
                nil,
            ),
        )
    }

    return &jsonLogger{
        output:      output,
        minLevel:    minLevel,
        levelLabels: labels,
    }
}

type jsonLogger struct {
    writeMutex  sync.Mutex
    output      io.Writer
    minLevel    loggingcontract.Level
    levelLabels loggingcontract.LevelLabels
    /* closed is atomic so Closed() can answer without the write lock: the probe is asked by the process-boundary exit handler, and an answer serialized behind an in-flight Write into a stalled pipe would hang the one handler that must reach os.Exit. The writes to the flag still happen under writeMutex — atomicity covers the lock-free read alone. */
    closed atomic.Bool
}

func (instance *jsonLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if priorityForLevel(level) < priorityForLevel(instance.minLevel) {
        return
    }

    label := instance.levelLabels.LabelFor(level)
    normalizedContext := normalizeJsonContext(context)

    timestamp := time.Now().Format(time.RFC3339)

    entry := logEntry{
        Message: message,
        Level:   label,
        Time:    timestamp,
        Context: normalizedContext,
    }

    encoded, err := json.Marshal(entry)
    if nil != err {
        fallback := map[string]any{
            "message":      message,
            "level":        label,
            "time":         timestamp,
            "marshalError": err.Error(),
        }

        encoded, _ = json.Marshal(fallback)
    }

    instance.writeMutex.Lock()
    defer instance.writeMutex.Unlock()

    if true == instance.closed.Load() {
        return
    }

    _, _ = instance.output.Write(append(encoded, '\n'))
}

func (instance *jsonLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *jsonLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *jsonLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *jsonLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *jsonLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

/* the close takes the same lock the writes take: it hands the writer to a Close that may mutate it, and a goroutine that outlives the container teardown can still be inside Write. The process console is recognized by identity — the os.Stdout and os.Stderr values themselves — and left open on purpose, so the flag is set only where the writer was really closed; the previous check compared the file name, which also matched a file the caller opened on "/dev/stdout", skipped the close it owed, and leaked that descriptor once per boot. */
func (instance *jsonLogger) Close() error {
    instance.writeMutex.Lock()
    defer instance.writeMutex.Unlock()

    if true == instance.closed.Load() {
        return nil
    }

    if os.Stdout == instance.output || os.Stderr == instance.output {
        return nil
    }

    closer, isCloser := instance.output.(io.Closer)
    if false == isCloser || true == internal.IsNilInterface(closer) {
        return nil
    }

    instance.closed.Store(true)

    return closer.Close()
}

/* Closed reports whether Close really closed the underlying writer, after which every Log call is silently dropped. A console logger never closes its stream and never reports true. The report lets whoever owns a final record — the process-boundary exit handler — refuse a logger that would swallow it and fall back to one that still writes. The read is lock-free on purpose: the caller is the exit handler, and an answer queued behind an in-flight Write into a stalled writer would hang the exit instead of informing it. */
func (instance *jsonLogger) Closed() bool {
    return instance.closed.Load()
}

var _ loggingcontract.Logger = (*jsonLogger)(nil)

func normalizeJsonContext(input map[string]any) map[string]any {
    if nil == input {
        return map[string]any{}
    }

    normalized := make(map[string]any, len(input))

    for key, value := range input {
        if nil == value {
            normalized[key] = nil
            continue
        }

        if err, ok := value.(error); true == ok {
            normalized[key] = err.Error()
            continue
        }

        normalized[key] = value
    }

    return normalized
}
