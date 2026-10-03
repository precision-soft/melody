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

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* jsonLogTimestampLayout is RFC 3339 with the nanosecond field at full width, so the stamps sort as text; time.RFC3339Nano trims trailing zeros. */
const jsonLogTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func NewJsonLogger(output io.Writer, minLevel loggingcontract.Level) loggingcontract.Logger {
    return NewJsonLoggerWithLabels(output, minLevel, loggingcontract.DefaultLevelLabels())
}

/* NewJsonLoggerWithClock stamps every record from the given clock instead of the process clock. The constructors without a clock read the process clock, since they also build the last-resort loggers, where no clock is injected. */
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

    return newJsonLogger(output, minLevel, labels, clockInstance)
}

func NewJsonLoggerWithLabels(output io.Writer, minLevel loggingcontract.Level, labels loggingcontract.LevelLabels) loggingcontract.Logger {
    return newJsonLogger(output, minLevel, labels, nil)
}

func newJsonLogger(
    output io.Writer,
    minLevel loggingcontract.Level,
    labels loggingcontract.LevelLabels,
    clockInstance clockcontract.Clock,
) *jsonLogger {
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
    copiedLabels := copyLevelLabels(labels)

    return &jsonLogger{
        output:             output,
        minLevel:           minLevel,
        levelLabels:        copiedLabels,
        encodedLevelLabels: encodeLevelLabels(copiedLabels),
        clock:              clockInstance,
    }
}

/* encodeLevelLabels encodes each configured label once, at construction, as LabelFor answers it for its level. */
func encodeLevelLabels(labels loggingcontract.LevelLabels) map[loggingcontract.Level][]byte {
    encoded := make(map[loggingcontract.Level][]byte, len(labels))
    for level := range labels {
        encoded[level] = encodeLevelLabel(labels.LabelFor(level))
    }

    return encoded
}

/* encodeLevelLabel cannot fail: LabelFor answers a label holding an int or a string, and the label's MarshalJSON encodes that value alone */
func encodeLevelLabel(label loggingcontract.LevelLabel) []byte {
    encoded, _ := json.Marshal(label)

    return encoded
}

func (instance *jsonLogger) encodedLevelLabel(level loggingcontract.Level, label loggingcontract.LevelLabel) []byte {
    if encoded, exists := instance.encodedLevelLabels[level]; true == exists {
        return encoded
    }

    return encodeLevelLabel(label)
}

type jsonLogger struct {
    writeMutex  sync.Mutex
    output      io.Writer
    minLevel    loggingcontract.Level
    levelLabels loggingcontract.LevelLabels
    /* read lock-free like levelLabels, and never written after construction */
    encodedLevelLabels map[loggingcontract.Level][]byte
    /* nil means the process clock, for the emergency and exit-path loggers that cannot be handed one */
    clock clockcontract.Clock
    /* closed is atomic so Closed answers without the write lock, which a Write into a stalled pipe could hold while the exit handler asks; the flag is still written under writeMutex */
    closed           atomic.Bool
    writeFailureOnce sync.Once
}

/* effectivePriorityForLevel weighs a level outside the five known ones as error, so an empty or unknown level is never dropped as debug; the label keeps the raw level. */
func effectivePriorityForLevel(level loggingcontract.Level) int {
    if false == IsValidLevel(level) {
        return priorityForLevel(loggingcontract.LevelError)
    }

    return priorityForLevel(level)
}

func (instance *jsonLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if effectivePriorityForLevel(level) < priorityForLevel(instance.minLevel) {
        return
    }

    label := instance.levelLabels.LabelFor(level)

    /* the normalization and the encoding stay outside the lock: they run the caller's MarshalJSON, String or Error, which is unbounded and may log through this very logger, so under the lock it would deadlock the journal */
    normalizedContext := normalizeJsonContext(context)
    encodedContext, contextMarshalErr := marshalJsonContextContained(normalizedContext)

    renderedContext := ""
    encodedMessage := []byte(nil)
    encodedLabel := []byte(nil)
    if nil != contextMarshalErr {
        renderedContext = renderJsonContextByKey(normalizedContext)
    } else {
        encodedMessage, _ = json.Marshal(message)
        encodedLabel = instance.encodedLevelLabel(level, label)
    }

    /* the stamp is taken under the write lock, so the stamps are in write order, and in UTC at fixed width, so their text order is their time order. Only already-encoded values are assembled under the lock, so it is held for bounded work. */
    instance.writeMutex.Lock()

    if true == instance.closed.Load() {
        instance.writeMutex.Unlock()

        return
    }

    instant := instance.currentInstant().UTC()

    encoded := []byte(nil)
    if nil == contextMarshalErr {
        /* the envelope is assembled from already-encoded parts: a json.RawMessage handed back to the encoder is re-validated one level deeper, so a context at the depth bound would be refused once wrapped */
        encoded = make([]byte, 0, len(encodedMessage)+len(encodedLabel)+len(jsonLogTimestampLayout)+len(encodedContext)+48)
        encoded = append(encoded, `{"message":`...)
        encoded = append(encoded, encodedMessage...)
        encoded = append(encoded, `,"level":`...)
        encoded = append(encoded, encodedLabel...)
        encoded = append(encoded, `,"time":"`...)
        encoded = instant.AppendFormat(encoded, jsonLogTimestampLayout)
        encoded = append(encoded, `","context":`...)
        encoded = append(encoded, encodedContext...)
        encoded = append(encoded, '}')
    } else {
        /* the fallback keeps the context as text, so one unmarshalable value does not cost every other key of a record describing a failure; every fallback value is a string, so the second marshal cannot fail */
        fallback := map[string]any{
            "message":      message,
            "level":        label,
            "time":         instant.Format(jsonLogTimestampLayout),
            "marshalError": contextMarshalErr.Error(),
            "context":      renderedContext,
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

func (instance *jsonLogger) currentInstant() time.Time {
    if nil == instance.clock {
        return time.Now()
    }

    return instance.clock.Now()
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

    return effectivePriorityForLevel(level) >= priorityForLevel(instance.minLevel)
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

/* jsonContextContainerKind names the plain shape the walk answered a value as; the container travels as a struct, since boxing a slice header into an interface would allocate per container on the hot path */
type jsonContextContainerKind int

const (
    jsonContextContainerNone jsonContextContainerKind = iota
    jsonContextContainerMap
    jsonContextContainerSlice
    jsonContextContainerMapSlice
)

type jsonContextContainer struct {
    kind          jsonContextContainerKind
    mapValue      map[string]any
    sliceValue    []any
    mapSliceValue []map[string]any
}

/* plainJsonContextContainer answers the value as a map[string]any, a []any or a []map[string]any when it can carry a cycle past the encoder, and the none kind otherwise. A value that marshals itself is not a container; a defined type over a plain shape, such as the exception contract's Context, is converted, keeping the backing pointer. */
func plainJsonContextContainer(value any) jsonContextContainer {
    switch typedValue := value.(type) {
    case map[string]any:
        return jsonContextContainer{kind: jsonContextContainerMap, mapValue: typedValue}

    /* the exception contract's Context is an alias of this one, so the single case covers both packages' context maps */
    case loggingcontract.Context:
        return jsonContextContainer{kind: jsonContextContainerMap, mapValue: typedValue}

    case []any:
        return jsonContextContainer{kind: jsonContextContainerSlice, sliceValue: typedValue}

    case []map[string]any:
        return jsonContextContainer{kind: jsonContextContainerMapSlice, mapSliceValue: typedValue}
    }

    if true == rendersItsOwnJson(value) {
        return jsonContextContainer{kind: jsonContextContainerNone}
    }

    reflectedValue := reflect.ValueOf(value)
    switch reflectedValue.Kind() {
    case reflect.Map:
        if true == reflectedValue.Type().ConvertibleTo(plainJsonContextMapType) {
            return jsonContextContainer{
                kind:     jsonContextContainerMap,
                mapValue: reflectedValue.Convert(plainJsonContextMapType).Interface().(map[string]any),
            }
        }
    case reflect.Slice:
        if true == reflectedValue.Type().ConvertibleTo(plainJsonContextSliceType) {
            return jsonContextContainer{
                kind:       jsonContextContainerSlice,
                sliceValue: reflectedValue.Convert(plainJsonContextSliceType).Interface().([]any),
            }
        }
    }

    return jsonContextContainer{kind: jsonContextContainerNone}
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
        if jsonContextContainerNone != plainJsonContextContainer(value).kind {
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

    container := plainJsonContextContainer(value)
    switch container.kind {
    case jsonContextContainerMap:
        return normalizeJsonContextMap(container.mapValue, remainingDepth, seen)

    case jsonContextContainerSlice:
        return normalizeJsonContextSlice(container.sliceValue, remainingDepth, seen)

    case jsonContextContainerMapSlice:
        normalized := make([]any, len(container.mapSliceValue))
        for index, element := range container.mapSliceValue {
            normalized[index] = normalizeJsonValue(element, remainingDepth-1, seen)
        }

        return normalized
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
