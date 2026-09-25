package logging

import (
    "encoding"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "reflect"
    "sort"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

/* jsonLogTimestampLayout is RFC 3339 with the nanosecond field at full width, so the stamps sort as text; time.RFC3339Nano trims trailing zeros. */
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

    /* the labels are copied: the map is read lock-free on every Log call, so a caller mutating the map it still holds would be a fatal concurrent map access */
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
    /* closed is atomic so Closed answers without the write lock, which a Write into a stalled pipe could hold while the exit handler asks; the flag is still written under writeMutex */
    closed           atomic.Bool
    writeFailureOnce sync.Once
}

func (instance *jsonLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    /* a level outside the five known ones weighs as error rather than as the lowest priority, so a zero-value exception carried by a recover handler, whose level is empty, passes a production threshold; the label keeps the raw level */
    effectivePriority := priorityForLevel(level)
    if false == IsValidLevel(level) {
        effectivePriority = priorityForLevel(loggingcontract.LevelError)
    }

    if effectivePriority < priorityForLevel(instance.minLevel) {
        return
    }

    label := instance.levelLabels.LabelFor(level)

    /* the normalization and the encoding stay outside the lock: they run the caller's MarshalJSON, String or Error, which is unbounded and may log through this very logger, so under the lock it would deadlock the journal */
    normalizedContext := normalizeJsonContext(context)
    encodedContext, contextMarshalErr := marshalJsonContextContained(normalizedContext)

    renderedContext := ""
    if nil != contextMarshalErr {
        renderedContext = renderJsonContextByKey(normalizedContext)
    }

    /* the stamp is taken under the write lock, so the stamps are in write order, and in UTC at fixed width, so their text order is their time order. Only already-encoded values are assembled under the lock, so it is held for bounded work. */
    instance.writeMutex.Lock()

    if true == instance.closed.Load() {
        instance.writeMutex.Unlock()

        return
    }

    timestamp := time.Now().UTC().Format(jsonLogTimestampLayout)

    encoded := []byte(nil)
    marshalErr := contextMarshalErr
    if nil == marshalErr {
        /* the envelope is assembled from parts already encoded rather than marshalled as one value: a json.RawMessage handed back to the encoder is re-validated one level deeper, so a context at the encoder's depth bound would be refused. Each remaining field is encoded on its own. */
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
        /* the fallback keeps the context as text, so one unmarshalable value costs that value alone and not the service name or the cause chain; every fallback value is a string, so the second marshal cannot fail */
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
        /* the encoder leaves the C1 block raw, so it is escaped before the write: a reader splitting on Unicode line boundaries or a terminal tailing the file then sees no control sequence */
        _, writeErr = instance.output.Write(append(internal.EscapeJsonC1Block(encoded), '\n'))
    }

    instance.writeMutex.Unlock()

    /* the echo is written after the lock is released: a stderr pipe nobody drains blocks, which under the lock would park every goroutine that logs, and Close with them */
    if nil != writeErr {
        instance.reportWriteFailure(writeErr)
    }
}

/* reportWriteFailure echoes the first failed write to stderr, once for the life of the logger, so a full or vanished log volume does not silence the journal with no signal. It writes to stderr directly, since EmergencyLogger would re-enter the failed Write, and stays silent when the output already is stderr. */
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

/* Close takes the lock the writes take, since a goroutine outliving the teardown can still be inside Write. The process console, recognized by identity as os.Stdout or os.Stderr, is left open, and the flag is set only where the writer was really closed. */
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

/* Closed reports whether Close really closed the underlying writer, after which every Log call is dropped; a console logger never reports true. The read is lock-free, so the exit handler asking it is never queued behind a Write into a stalled writer. */
func (instance *jsonLogger) Closed() bool {
    return instance.closed.Load()
}

/* Enabled answers with the arithmetic Log applies: a level below the threshold is dropped and an invalid level weighs as error. A closed logger reports nothing enabled. */
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

/* normalizeJsonContextMaxDepth bounds the recursive normalization, since a deep enough acyclic context would overflow the goroutine stack, a fatal error no recover reaches. It is a backstop: the shapes this package nests sit two or three levels down. */
const normalizeJsonContextMaxDepth = 10000

/* the cycle and depth markers keep what reaches the encoder finite: a surviving cycle would route the record into the fmt fallback, and fmt has no cycle detection */
const normalizeJsonContextCycleMarker = "<cycle>"
const normalizeJsonContextDepthMarker = "<depth limit>"

/* the plain shapes the walk descends into; a defined type over one of them is converted, which keeps the backing pointer the cycle keying reads */
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

/* normalizeJsonContextMap keys containers on the current path only, so a map named from two sibling keys is a lattice, not a cycle, and renders whole. */
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

/* rendersItsOwnJson answers whether the encoder renders the value through a method of its own rather than by its shape: a value that answers it is not a container the walk descends into, at any depth, whatever its underlying type */
func rendersItsOwnJson(value any) bool {
    if _, isMarshaler := value.(json.Marshaler); true == isMarshaler {
        return true
    }

    _, isTextMarshaler := value.(encoding.TextMarshaler)

    return isTextMarshaler
}

/* isJsonContextContainer answers for the shapes the walk descends into, which are exactly the ones that can carry a cycle past the encoder */
func isJsonContextContainer(value any) bool {
    switch value.(type) {
    case map[string]any, loggingcontract.Context, []any, []map[string]any:
        return true
    }

    if true == rendersItsOwnJson(value) {
        return false
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

/* normalizeJsonValue renders every error in the context as its message at any depth, since the encoder marshals an error as an empty object; a typed nil renders as null. It descends only the map and slice shapes this package produces. */
func normalizeJsonValue(value any, remainingDepth int, seen map[jsonContextVisitKey]struct{}) any {
    if nil == value {
        return nil
    }

    if 0 >= remainingDepth {
        /* the floor bounds the descent, not the conversion: an error at the floor still renders as its message */
        if err, ok := value.(error); true == ok && false == internal.IsNilInterface(err) {
            if _, isMarshaler := value.(json.Marshaler); false == isMarshaler {
                return recoveredErrorMessage(err)
            }
        }

        /* a container at the floor is not handed on, since nothing has walked it for a cycle; a scalar passes through */
        if true == isJsonContextContainer(value) {
            return normalizeJsonContextDepthMarker
        }

        return value
    }

    if err, ok := value.(error); true == ok {
        if true == internal.IsNilInterface(err) {
            return nil
        }

        /* an error that marshals itself keeps its structural rendering; every other error renders as its message */
        if _, isMarshaler := value.(json.Marshaler); true == isMarshaler {
            return value
        }

        return recoveredErrorMessage(err)
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

    /* a value that marshals itself, a redacting map or slice type among them, is handed to the encoder as it is: the conversion below would strip the methods that say how it renders */
    if true == rendersItsOwnJson(value) {
        return value
    }

    /* a defined type over one of the shapes above, the exception contract's Context among them, fails every assertion above; converting it keeps the backing pointer, so the cycle keying still sees it */
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

/* marshalJsonContextContained encodes the context under a recover: the encoder re-raises a panic from the caller's own marshalers, and a record is written from the recovery defers, where a second panic would pass the one being reported. A panicking value falls back to the text rendering. */
func marshalJsonContextContained(normalizedContext any) (encoded []byte, marshalErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        encoded = nil
        marshalErr = errors.New("encoding the context panicked: " + describeRecoveredValue(recoveredValue))
    }()

    return json.Marshal(normalizedContext)
}

/* renderJsonContextByKey is the text a record carries when its context does not encode as a whole: each key is encoded on its own, sorted, and a refused key reads as its reason. It does not use fmt, which has no cycle detection; the encoder refuses a cycle held through a struct field. */
func renderJsonContextByKey(normalizedContext map[string]any) string {
    keyList := make([]string, 0, len(normalizedContext))
    for key := range normalizedContext {
        keyList = append(keyList, key)
    }

    sort.Strings(keyList)

    rendered := make([]byte, 0, 64)
    rendered = append(rendered, '{')

    for index, key := range keyList {
        if 0 < index {
            rendered = append(rendered, ',')
        }

        encodedKey, _ := json.Marshal(key)
        rendered = append(rendered, encodedKey...)
        rendered = append(rendered, ':')

        encodedValue, valueErr := marshalJsonContextContained(normalizedContext[key])
        if nil != valueErr {
            encodedValue, _ = json.Marshal("<unencodable: " + valueErr.Error() + ">")
        }

        rendered = append(rendered, encodedValue...)
    }

    rendered = append(rendered, '}')

    return string(rendered)
}
