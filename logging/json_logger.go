package logging

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

    /* the labels are copied because the caller keeps its reference: the map is read lock-free on every Log call, and a caller mutating the map it still holds against those reads is a fatal concurrent map access no recover reaches — the copy makes the reads safe the way the immutable output and minLevel fields already are */
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
    /* closed is atomic so Closed() can answer without the write lock: the probe is asked by the process-boundary exit handler, and an answer serialized behind an in-flight Write into a stalled pipe would hang the one handler that must reach os.Exit. The writes to the flag still happen under writeMutex — atomicity covers the lock-free read alone. */
    closed           atomic.Bool
    writeFailureOnce sync.Once
}

func (instance *jsonLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    /* a level outside the five known ones weighs as error instead of inheriting the default lowest priority: the unknown level is the case that least deserves silence, and the zero-value exception a recover handler can carry reaches this line with an empty level — filed as debug, its fatal record was dropped by every production threshold. The label keeps the raw level, so the record still says what it was handed. */
    effectivePriority := priorityForLevel(level)
    if false == IsValidLevel(level) {
        effectivePriority = priorityForLevel(loggingcontract.LevelError)
    }

    if effectivePriority < priorityForLevel(instance.minLevel) {
        return
    }

    label := instance.levelLabels.LabelFor(level)

    /* the normalization is the one step that stays outside the lock: it walks the caller's own context, which is the unbounded part of the work and touches nothing this logger shares. */
    normalizedContext := normalizeJsonContext(context)

    /* the stamp and the encoding both happen under the write lock, and the order of the stamps is therefore the order of the writes. Taken above the lock, the stamp said when the record was FORMED, and the two orders diverged by however long the encoding took: measured at eight goroutines writing records of a dozen keys, 484 of 1600 records reached the file out of stamp order, while LOGGING.md promises the ordering is reconstructible from the stamps and the comment on this very line claimed the precision was what the write mutex paid for. Nanosecond precision keeps that ordering legible once it is true — whole-second stamps made every record of a busy second indistinguishable, and the fraction still parses under the RFC 3339 layouts consumers already use.

    The cost is the encoding serialized across writers: measured at eight goroutines against a real file, 8648 ns per record became 13755. A logger writes to one destination through one lock either way, so what this buys is the one ordering guarantee the document already sold. */
    instance.writeMutex.Lock()
    defer instance.writeMutex.Unlock()

    if true == instance.closed.Load() {
        return
    }

    timestamp := time.Now().Format(time.RFC3339Nano)

    entry := logEntry{
        Message: message,
        Level:   label,
        Time:    timestamp,
        Context: normalizedContext,
    }

    encoded, err := json.Marshal(entry)
    if nil != err {
        /* the fallback keeps the context as text rather than dropping it: one unmarshalable value used to cost every other key of the record — the service name, the cause chain — exactly when the record described a failure. Every fallback value is a string, so the second marshal cannot fail. */
        fallback := map[string]any{
            "message":      message,
            "level":        label,
            "time":         timestamp,
            "marshalError": err.Error(),
            "context":      fmt.Sprintf("%+v", normalizedContext),
        }

        encoded, _ = json.Marshal(fallback)
    }

    if 0 == len(encoded) {
        return
    }

    _, writeErr := instance.output.Write(append(encoded, '\n'))
    if nil != writeErr {
        instance.reportWriteFailure(writeErr)
    }
}

/* reportWriteFailure echoes the first failed write to stderr, once for the life of the logger. A var/log that is full, read-only or on a vanished mount otherwise silences the entire journal with no signal on any channel — the operator reads a healthy-looking empty file — while the other way this same function can fail, a value that will not marshal, has had its fallback since it was written.

It writes to stderr directly rather than through EmergencyLogger, which is itself a jsonLogger and would re-enter the very Write that just failed, and it stays silent when this logger's own output already IS stderr, where the echo would be a second attempt at the destination that refused the first. Once, because a logger writing into a full disk fails on every record it is given, and a per-record echo would move the flood from one channel to the other rather than report it. */
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

/* normalizeJsonContextMaxDepth bounds the recursive normalization: the shapes this package itself nests — a cause context chain holding provider maps holding error values — sit two or three levels down, and the cap keeps a pathological self-referencing structure from recursing without end. A value below the cap is passed to the encoder as it is. */
const normalizeJsonContextMaxDepth = 6

func normalizeJsonContext(input map[string]any) map[string]any {
    if nil == input {
        return map[string]any{}
    }

    return normalizeJsonMap(input, normalizeJsonContextMaxDepth)
}

func normalizeJsonMap(input map[string]any, remainingDepth int) map[string]any {
    normalized := make(map[string]any, len(input))

    for key, value := range input {
        normalized[key] = normalizeJsonValue(value, remainingDepth)
    }

    return normalized
}

/* normalizeJsonValue renders every error in the context as its message, however deep the containers this package nests put it: an error left to the encoder marshals as an empty object — every field unexported, no marshaler — so a cause carried inside a chain entry survived as "{}" while the same error one level up rendered fine. A typed nil is the nil its producer meant and renders as null instead of panicking on the Error call; the normalization descends the map and slice shapes the package itself produces and leaves every other value to the encoder. */
func normalizeJsonValue(value any, remainingDepth int) any {
    if nil == value {
        return nil
    }

    if 0 >= remainingDepth {
        return value
    }

    if err, ok := value.(error); true == ok {
        if true == internal.IsNilInterface(err) {
            return nil
        }

        /* an error that also marshals itself opted into a structural rendering — a validation error collection says the same thing here that it says in the response body — while every other error still renders as its message, because the encoder's default for an error is the empty object */
        if _, isMarshaler := value.(json.Marshaler); true == isMarshaler {
            return value
        }

        return err.Error()
    }

    switch typedValue := value.(type) {
    case map[string]any:
        return normalizeJsonMap(typedValue, remainingDepth-1)

    /* the exception contract's Context is an alias of this one, so the single case covers the context maps both packages put into a record */
    case loggingcontract.Context:
        return normalizeJsonMap(typedValue, remainingDepth-1)

    case []any:
        normalized := make([]any, len(typedValue))
        for index, element := range typedValue {
            normalized[index] = normalizeJsonValue(element, remainingDepth-1)
        }

        return normalized

    case []map[string]any:
        normalized := make([]any, len(typedValue))
        for index, element := range typedValue {
            normalized[index] = normalizeJsonMap(element, remainingDepth-1)
        }

        return normalized
    }

    return value
}
