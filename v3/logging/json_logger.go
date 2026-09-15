package logging

import (
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "reflect"
    "sync"
    "sync/atomic"
    "time"

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const jsonLogTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func NewJsonLogger(output io.Writer, minLevel loggingcontract.Level) loggingcontract.Logger {
    return NewJsonLoggerWithLabels(output, minLevel, loggingcontract.DefaultLevelLabels())
}

/* NewJsonLoggerWithClock stamps every record from the given clock instead of the process clock, which is what lets a test freeze the journal's time and what makes the container-built logger agree with every other timestamp the kernel's clock produces. The two constructors without a clock keep reading the process clock, because they are also the constructors of the last-resort loggers — the emergency logger on stderr, the exit-path logger of a dying process — where no injected clock exists to be wrong. */
func NewJsonLoggerWithClock(
    output io.Writer,
    minLevel loggingcontract.Level,
    labels loggingcontract.LevelLabels,
    clockInstance clockcontract.Clock,
) loggingcontract.Logger {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("json logger clock is not provided", nil, nil),
        )
    }

    logger := NewJsonLoggerWithLabels(output, minLevel, labels)
    logger.(*jsonLogger).clock = clockInstance

    return logger
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

    copiedLabels := make(loggingcontract.LevelLabels, len(labels))
    for level, label := range labels {
        copiedLabels[level] = label
    }

    return &jsonLogger{
        output:      output,
        minLevel:    minLevel,
        levelLabels: copiedLabels,
    }
}

type jsonLogger struct {
    writeMutex  sync.Mutex
    output      io.Writer
    minLevel    loggingcontract.Level
    levelLabels loggingcontract.LevelLabels

    clock clockcontract.Clock

    closed           atomic.Bool
    writeFailureOnce sync.Once
}

func (instance *jsonLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {

    effectivePriority := priorityForLevel(level)
    if false == IsValidLevel(level) {
        effectivePriority = priorityForLevel(loggingcontract.LevelError)
    }

    if effectivePriority < priorityForLevel(instance.minLevel) {
        return
    }

    label := instance.levelLabels.LabelFor(level)

    normalizedContext := normalizeJsonContext(context)
    encodedContext, contextMarshalErr := json.Marshal(normalizedContext)

    renderedContext := ""
    if nil != contextMarshalErr {

        renderedContext = fmt.Sprintf("%+v", normalizedContext)
    }

    instance.writeMutex.Lock()

    if true == instance.closed.Load() {
        instance.writeMutex.Unlock()

        return
    }

    timestamp := instance.currentInstant().UTC().Format(jsonLogTimestampLayout)

    encoded := []byte(nil)
    marshalErr := contextMarshalErr
    if nil == marshalErr {

        encodedMessage, messageErr := json.Marshal(message)
        encodedLabel, labelErr := json.Marshal(label)
        encodedTimestamp, timestampErr := json.Marshal(timestamp)

        marshalErr = errors.Join(messageErr, labelErr, timestampErr)
        if nil == marshalErr {
            encoded = append(encoded, `{"message":`...)
            encoded = append(encoded, encodedMessage...)
            encoded = append(encoded, `,"level":`...)
            encoded = append(encoded, encodedLabel...)
            encoded = append(encoded, `,"time":`...)
            encoded = append(encoded, encodedTimestamp...)
            encoded = append(encoded, `,"context":`...)
            encoded = append(encoded, encodedContext...)
            encoded = append(encoded, '}')
        }
    }

    if nil != marshalErr {

        renderedFallbackContext := renderedContext
        if nil == contextMarshalErr {
            renderedFallbackContext = "the record could not be assembled from its already-encoded parts"
        }

        fallback := map[string]any{
            "message":      message,
            "level":        label,
            "time":         timestamp,
            "marshalError": marshalErr.Error(),
            "context":      renderedFallbackContext,
        }

        encoded, _ = json.Marshal(fallback)
    }

    writeErr := error(nil)
    if 0 < len(encoded) {

        _, writeErr = instance.output.Write(append(internal.EscapeJsonC1Block(encoded), '\n'))
    }

    instance.writeMutex.Unlock()

    if nil != writeErr {
        instance.reportWriteFailure(writeErr)
    }
}

func (instance *jsonLogger) currentInstant() time.Time {
    if nil == instance.clock {
        return time.Now()
    }

    return instance.clock.Now()
}

func (instance *jsonLogger) reportWriteFailure(writeErr error) {
    if os.Stderr == instance.output {
        return
    }

    instance.writeFailureOnce.Do(func() {
        fmt.Fprintf(os.Stderr, "melody: the json logger failed to write a record to its output: %v\n", writeErr)
    })
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

/* Close serializes with writes and closes an owned writer. The exact os.Stdout and os.Stderr objects remain open; separately opened descriptors are closed normally. */
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

/* Enabled answers the same question Log asks itself first, and answers it with the same arithmetic: a level below the configured threshold is dropped, and an invalid level weighs as error rather than as the lowest priority for the reason written at that branch. A closed logger writes nothing at all, so it reports nothing enabled — the caller asking is about to build a record, and a record built for a logger that has stopped writing is pure waste. */
func (instance *jsonLogger) Enabled(level loggingcontract.Level) bool {
    if true == instance.closed.Load() {
        return false
    }

    effectivePriority := priorityForLevel(level)
    if false == IsValidLevel(level) {
        effectivePriority = priorityForLevel(loggingcontract.LevelError)
    }

    return effectivePriority >= priorityForLevel(instance.minLevel)
}

var _ loggingcontract.Logger = (*jsonLogger)(nil)
var _ loggingcontract.LevelReporter = (*jsonLogger)(nil)

const normalizeJsonContextMaxDepth = 10000

const normalizeJsonContextCycleMarker = "<cycle>"
const normalizeJsonContextDepthMarker = "<depth limit>"

var plainJsonContextMapType = reflect.TypeOf(map[string]any(nil))
var plainJsonContextSliceType = reflect.TypeOf([]any(nil))

type jsonContextVisitKey struct {
    pointer uintptr
    length  uintptr
}

func normalizeJsonContext(input map[string]any) map[string]any {
    if nil == input {
        return map[string]any{}
    }

    return normalizeJsonMap(input, normalizeJsonContextMaxDepth, map[jsonContextVisitKey]struct{}{})
}

func normalizeJsonMap(input map[string]any, remainingDepth int, seen map[jsonContextVisitKey]struct{}) map[string]any {
    normalized := make(map[string]any, len(input))

    for key, value := range input {
        normalized[key] = normalizeJsonValue(value, remainingDepth, seen)
    }

    return normalized
}

func normalizeJsonContextMap(
    value map[string]any,
    remainingDepth int,
    seen map[jsonContextVisitKey]struct{},
) any {
    key := jsonContextVisitKey{pointer: reflect.ValueOf(value).Pointer()}
    if _, visited := seen[key]; true == visited {
        return normalizeJsonContextCycleMarker
    }

    seen[key] = struct{}{}
    defer delete(seen, key)

    return normalizeJsonMap(value, remainingDepth-1, seen)
}

func normalizeJsonContextSlice(
    value []any,
    remainingDepth int,
    seen map[jsonContextVisitKey]struct{},
) any {
    pointer := reflect.ValueOf(value).Pointer()
    if 0 != pointer {
        key := jsonContextVisitKey{pointer: pointer, length: uintptr(len(value)) + 1}
        if _, visited := seen[key]; true == visited {
            return normalizeJsonContextCycleMarker
        }

        seen[key] = struct{}{}
        defer delete(seen, key)
    }

    normalized := make([]any, len(value))
    for index, element := range value {
        normalized[index] = normalizeJsonValue(element, remainingDepth-1, seen)
    }

    return normalized
}

func isJsonContextContainer(value any) bool {
    switch value.(type) {
    case map[string]any, loggingcontract.Context, []any, []map[string]any:
        return true
    }

    reflectedValue := reflect.ValueOf(value)
    switch reflectedValue.Kind() {
    case reflect.Map:
        return reflectedValue.Type().ConvertibleTo(plainJsonContextMapType)
    case reflect.Slice:
        return reflectedValue.Type().ConvertibleTo(plainJsonContextSliceType)
    }

    return false
}

func normalizeJsonValue(value any, remainingDepth int, seen map[jsonContextVisitKey]struct{}) any {
    if nil == value {
        return nil
    }

    if 0 >= remainingDepth {

        if err, ok := value.(error); true == ok && false == internal.IsNilInterface(err) {
            if _, isMarshaler := value.(json.Marshaler); false == isMarshaler {
                return err.Error()
            }
        }

        if true == isJsonContextContainer(value) {
            return normalizeJsonContextDepthMarker
        }

        return value
    }

    if err, ok := value.(error); true == ok {
        if true == internal.IsNilInterface(err) {
            return nil
        }

        if _, isMarshaler := value.(json.Marshaler); true == isMarshaler {
            return value
        }

        return err.Error()
    }

    switch typedValue := value.(type) {
    case map[string]any:
        return normalizeJsonContextMap(typedValue, remainingDepth, seen)

    case loggingcontract.Context:
        return normalizeJsonContextMap(typedValue, remainingDepth, seen)

    case []any:
        return normalizeJsonContextSlice(typedValue, remainingDepth, seen)

    case []map[string]any:
        normalized := make([]any, len(typedValue))
        for index, element := range typedValue {
            normalized[index] = normalizeJsonValue(element, remainingDepth-1, seen)
        }

        return normalized
    }

    reflectedValue := reflect.ValueOf(value)
    switch reflectedValue.Kind() {
    case reflect.Map:
        if true == reflectedValue.Type().ConvertibleTo(plainJsonContextMapType) {
            converted := reflectedValue.Convert(plainJsonContextMapType).Interface().(map[string]any)

            return normalizeJsonContextMap(converted, remainingDepth, seen)
        }
    case reflect.Slice:
        if true == reflectedValue.Type().ConvertibleTo(plainJsonContextSliceType) {
            converted := reflectedValue.Convert(plainJsonContextSliceType).Interface().([]any)

            return normalizeJsonContextSlice(converted, remainingDepth, seen)
        }
    }

    return value
}
