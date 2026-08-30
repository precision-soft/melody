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

    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

/* jsonLogTimestampLayout is RFC 3339 with the nanosecond field written to its full width. time.RFC3339Nano trims trailing zeros, which makes the field variable width and the stamps unsortable as text — the whole point of taking the stamp under the write lock. */
const jsonLogTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

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

    /* the normalization and the encoding of the caller's own context both stay outside the lock. Both run application code — a MarshalJSON, a String, an Error of the caller's own — which is unbounded work this logger does not own and which may itself log through this very logger: under the lock, such a value deadlocked the whole journal on its own record, and every other writer queued behind the slowest caller's encoder. */
    normalizedContext := normalizeJsonContext(context)
    encodedContext, contextMarshalErr := json.Marshal(normalizedContext)

    renderedContext := ""
    if nil != contextMarshalErr {
        renderedContext = fmt.Sprintf("%+v", normalizedContext)
    }

    /* the stamp is taken under the write lock, so the order of the stamps is the order of the writes: taken above the lock it said when the record was FORMED, and the two orders diverged by however long the encoding took — measured at eight goroutines writing records of a dozen keys, 484 of 1600 records reached the file out of stamp order, while LOGGING.md sells the ordering as reconstructible from the stamps.

       The layout is FIXED WIDTH, and that is the half without which the rest buys nothing: RFC3339Nano trims trailing zeros, so a record landing on a whole second renders with no fraction at all and sorts after every fractional record of that same second — '.' is 0x2E and 'Z' is 0x5A. Measured on four instants one second apart, the lexical order was not the chronological one. The instant is rendered in UTC for the same reason, so a zone whose offset moves cannot reorder two stamps either.

       What stays under the lock is assembly of values that are already encoded, plus the write; no application code runs here, so the lock is held for bounded work. */
    instance.writeMutex.Lock()

    if true == instance.closed.Load() {
        instance.writeMutex.Unlock()

        return
    }

    timestamp := time.Now().UTC().Format(jsonLogTimestampLayout)

    encoded := []byte(nil)
    marshalErr := contextMarshalErr
    if nil == marshalErr {
        /* the envelope is assembled from parts that are already encoded rather than marshalled as one value: handing the encoded context back to the encoder as a json.RawMessage makes it re-validate the bytes it just produced, and it does so one level deeper than it wrote them, so a context that fits the encoder's depth bound is refused when it is wrapped. Each of the three remaining fields is encoded on its own, so the escaping is the encoder's and not this line's. */
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
        /* the fallback keeps the context as text rather than dropping it: one unmarshalable value used to cost every other key of the record — the service name, the cause chain — exactly when the record described a failure. Every fallback value is a string, so the second marshal cannot fail. The rendering itself was done above the lock, where the caller's own String methods belong; an outer failure over already-encoded values leaves nothing of the caller to render, so it says so. */
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
        _, writeErr = instance.output.Write(append(encoded, '\n'))
    }

    instance.writeMutex.Unlock()

    /* the echo is taken after the lock is released: it writes to stderr, and a stderr that is a pipe nobody drains blocks — under the lock that parked every goroutine that logs, and Close with them, on the one channel that exists to report that the journal has stopped working. */
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

/* normalizeJsonContextMaxDepth bounds the recursive normalization. The cycle keying below answers the context that holds itself; this answers the one that is merely very deep, which nothing else does — a deep enough acyclic context walks until the goroutine stack is gone, and a stack overflow is a fatal error that no recover turns into a reported failure. It is a backstop rather than a shape bound: the shapes this package nests — a cause context chain holding provider maps holding error values — sit two or three levels down. */
const normalizeJsonContextMaxDepth = 10000

/* the two markers say different things to whoever reads the rendered record: a cycle is a structure that closes on itself, the depth marker is one that goes deeper than anything worth printing. Both exist so that what reaches the encoder is finite — a cycle that survives this walk makes json.Marshal answer with its cycle ERROR, which routes the record into the fmt fallback in Log, and fmt has no cycle detection of its own: the fallback written to save the record kills the process instead. */
const normalizeJsonContextCycleMarker = "<cycle>"
const normalizeJsonContextDepthMarker = "<depth limit>"

/* the plain shapes the walk descends into; a defined type sharing their underlying type is converted to them below, which keeps the backing pointer and so the cycle keying */
var plainJsonContextMapType = reflect.TypeOf(map[string]any(nil))
var plainJsonContextSliceType = reflect.TypeOf([]any(nil))

/* the length distinguishes two slices that share a backing array, which are the same container for the walk only when they span the same elements */
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

/* the walk keys the containers on the CURRENT path rather than every container it has ever seen: a context that names the same map from two sibling keys is a lattice, not a cycle, and renders whole. */
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

/* isJsonContextContainer answers for the shapes the walk descends into, which are exactly the ones that can carry a cycle past the encoder */
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

/* normalizeJsonValue renders every error in the context as its message, however deep the containers this package nests put it: an error left to the encoder marshals as an empty object — every field unexported, no marshaler — so a cause carried inside a chain entry survived as "{}" while the same error one level up rendered fine. A typed nil is the nil its producer meant and renders as null instead of panicking on the Error call; the normalization descends the map and slice shapes the package itself produces and leaves every other value to the encoder. */
func normalizeJsonValue(value any, remainingDepth int, seen map[jsonContextVisitKey]struct{}) any {
    if nil == value {
        return nil
    }

    if 0 >= remainingDepth {
        /* the floor bounds the DESCENT, not the scalar conversion: an error sitting at the floor still renders as its message, because handed to the encoder raw it marshals as the empty object — losing exactly the failure the record nested this deep to carry */
        if err, ok := value.(error); true == ok && false == internal.IsNilInterface(err) {
            if _, isMarshaler := value.(json.Marshaler); false == isMarshaler {
                return err.Error()
            }
        }

        /* a container is the one value the floor may not hand on as it is: nothing has walked what it holds, so a cycle closing below the bound would reach the encoder — and from there the fmt fallback that cannot survive one. A scalar carries no such risk and passes through. */
        if true == isJsonContextContainer(value) {
            return normalizeJsonContextDepthMarker
        }

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
        return normalizeJsonContextMap(typedValue, remainingDepth, seen)

    /* the exception contract's Context is an alias of this one, so the single case covers the context maps both packages put into a record */
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

    /* a defined type whose underlying type is one of the shapes above — the exception contract's Context is one, and it is what a producer reaches for when nesting structured data — fails every assertion above while carrying the same shape. Left unconverted it rides past the cycle keying, and the cycle it holds reaches the encoder. The conversion keeps the backing pointer, which is what the keying is built on. */
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
