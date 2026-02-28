package logging

import (
    "encoding/json"
    "io"
    "os"
    "time"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type jsonLogger struct {
    output   io.Writer
    minLevel loggingcontract.Level
}

func NewJsonLogger(output io.Writer, minLevel loggingcontract.Level) loggingcontract.Logger {
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
        output:   output,
        minLevel: minLevel,
    }
}

func (instance *jsonLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if priorityForLevel(level) < priorityForLevel(instance.minLevel) {
        return
    }

    normalizedContext := normalizeJsonContext(context)

    entry := logEntry{
        Message: message,
        Level:   level,
        Time:    time.Now().Format(time.RFC3339),
        Context: normalizedContext,
    }

    encoded, err := json.Marshal(entry)
    if nil != err {
        fallback := map[string]any{
            "message":      message,
            "level":        string(level),
            "time":         time.Now().Format(time.RFC3339),
            "marshalError": err.Error(),
        }

        encoded, _ = json.Marshal(fallback)
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

func (instance *jsonLogger) Close() error {
    file, isFile := instance.output.(*os.File)
    if true == isFile && nil != file {
        fileName := file.Name()
        if "/dev/stdout" == fileName || "/dev/stderr" == fileName {
            return nil
        }
    }

    closer, isCloser := instance.output.(io.Closer)
    if false == isCloser || true == internal.IsNilInterface(closer) {
        return nil
    }

    return closer.Close()
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
