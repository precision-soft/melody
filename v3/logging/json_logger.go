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

/* jsonLogTimestampLayout is RFC 3339 with the nanosecond field written to its full width. time.RFC3339Nano trims trailing zeros, which makes the field variable width and the stamps unsortable as text — the whole point of taking the stamp under the write lock. */
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

    /* the labels are copied because the caller keeps its reference: the map is read lock-free on every Log call, and a caller mutating the map it still holds against those reads is a fatal concurrent map access no recover reaches — the copy makes the reads safe the way the immutable output and minLevel fields already are */
    copiedLabels := copyLevelLabels(labels)

    return &jsonLogger{
        output:             output,
        minLevel:           minLevel,
        levelLabels:        copiedLabels,
        encodedLevelLabels: encodeLevelLabels(copiedLabels),
        clock:              clockInstance,
    }
}

/* encodeLevelLabels encodes each configured label once, at construction, as LabelFor answers it for its level, so a record of a configured level carries bytes encoded before any record was written */
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
    /* nil means the process clock: the constructors that cannot be handed a clock — the emergency and exit-path loggers — still have to stamp their records */
    clock clockcontract.Clock
    /* closed is atomic so Closed() can answer without the write lock: the probe is asked by the process-boundary exit handler, and an answer serialized behind an in-flight Write into a stalled pipe would hang the one handler that must reach os.Exit. The writes to the flag still happen under writeMutex — atomicity covers the lock-free read alone. */
    closed           atomic.Bool
    writeFailureOnce sync.Once
}

/* effectivePriorityForLevel weighs a level outside the five known ones as error instead of inheriting the default lowest priority: the unknown level is the case that least deserves silence, and the zero-value exception a recover handler can carry reaches the logger with an empty level — filed as debug, its fatal record was dropped by every production threshold. The label keeps the raw level, so the record still says what it was handed. */
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

    /* the normalization and the encoding of the caller's own context both stay outside the lock. Both run application code — a MarshalJSON, a String, an Error of the caller's own — which is unbounded work this logger does not own and which may itself log through this very logger: under the lock, such a value deadlocked the whole journal on its own record, and every other writer queued behind the slowest caller's encoder. The message is encoded out here too: a string always encodes, and a long one is work the other writers need not queue behind. */
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

    /* the stamp is taken under the write lock, so the order of the stamps is the order of the writes: taken above the lock it said when the record was FORMED, and the two orders diverged by however long the encoding took — measured at eight goroutines writing records of a dozen keys, 484 of 1600 records reached the file out of stamp order, while LOGGING.md sells the ordering as reconstructible from the stamps.

       The layout is FIXED WIDTH, and that is the half without which the rest buys nothing: RFC3339Nano trims trailing zeros, so a record landing on a whole second renders with no fraction at all and sorts after every fractional record of that same second — '.' is 0x2E and 'Z' is 0x5A. Measured on four instants one second apart, the lexical order was not the chronological one. The instant is rendered in UTC for the same reason, so a zone whose offset moves cannot reorder two stamps either.

       What stays under the lock is assembly of values that are already encoded, plus the write; no application code runs here, so the lock is held for bounded work. */
    instance.writeMutex.Lock()

    if true == instance.closed.Load() {
        instance.writeMutex.Unlock()

        return
    }

    instant := instance.currentInstant().UTC()

    encoded := []byte(nil)
    if nil == contextMarshalErr {
        /* the envelope is assembled from parts that are already encoded rather than marshalled as one value: handing the encoded context back to the encoder as a json.RawMessage makes it re-validate the bytes it just produced, and it does so one level deeper than it wrote them, so a context that fits the encoder's depth bound is refused when it is wrapped. The stamp is appended between its quotes as it is formatted: the layout in UTC writes digits, '-', ':', 'T', '.' and 'Z' alone, none of which the encoder would escape. The capacity covers the whole line, the line end the write appends included. */
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
        /* the fallback keeps the context as text rather than dropping it: one unmarshalable value used to cost every other key of the record — the service name, the cause chain — exactly when the record described a failure. Every fallback value is a string or the label, so the second marshal cannot fail. The rendering itself was done above the lock, where the caller's own String methods belong. */
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
        /* the encoder leaves the C1 block raw in every field it wrote, so the record is spelled once more before the write: the escape decodes to the same rune, and a reader that splits on Unicode line boundaries or a terminal the file is tailed on sees no control sequence in the bytes */
        _, writeErr = instance.output.Write(append(internal.EscapeJsonC1Block(encoded), '\n'))
    }

    instance.writeMutex.Unlock()

    /* the echo is taken after the lock is released: it writes to stderr, and a stderr that is a pipe nobody drains blocks — under the lock that parked every goroutine that logs, and Close with them, on the one channel that exists to report that the journal has stopped working. */
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

/* Enabled answers the same question Log asks itself first, and answers it with the same arithmetic: a level below the configured threshold is dropped, and an invalid level weighs as error rather than as the lowest priority. A closed logger writes nothing at all, so it reports nothing enabled — the caller asking is about to build a record, and a record built for a logger that has stopped writing is pure waste. */
func (instance *jsonLogger) Enabled(level loggingcontract.Level) bool {
    if true == instance.closed.Load() {
        return false
    }

    return effectivePriorityForLevel(level) >= priorityForLevel(instance.minLevel)
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

/* rendersItsOwnJson answers whether the encoder renders the value through a method of its own rather than by its shape: a value that answers it is not a container the walk descends into, at any depth, whatever its underlying type */
func rendersItsOwnJson(value any) bool {
    if _, isMarshaler := value.(json.Marshaler); true == isMarshaler {
        return true
    }

    _, isTextMarshaler := value.(encoding.TextMarshaler)

    return isTextMarshaler
}

/* jsonContextContainerKind names which of the plain shapes the walk descends into a value was answered as; the container travels as a struct rather than as an interface, because boxing a slice header back into an interface costs an allocation per container on the hot path */
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

/* plainJsonContextContainer answers the value in the plain shape the walk descends into — a map[string]any, a []any or a []map[string]any — for exactly the values that can carry a cycle past the encoder, and the none kind for every other value.

   A value that marshals itself — a map or slice type carrying its own MarshalJSON or MarshalText, a redacting one above all — said how it renders, and the conversion would strip exactly the methods that say it: it is not a container, at any depth. A defined type whose underlying type is one of the plain shapes — the exception contract's Context is one, and it is what a producer reaches for when nesting structured data — fails the assertions while carrying the same shape; left unconverted it rides past the cycle keying, and the cycle it holds reaches the encoder. The conversion keeps the backing pointer, which is what the keying is built on. */
func plainJsonContextContainer(value any) jsonContextContainer {
    switch typedValue := value.(type) {
    case map[string]any:
        return jsonContextContainer{kind: jsonContextContainerMap, mapValue: typedValue}

    /* the exception contract's Context is an alias of this one, so the single case covers the context maps both packages put into a record */
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

/* normalizeJsonValue renders every error in the context as its message, however deep the containers this package nests put it: an error left to the encoder marshals as an empty object — every field unexported, no marshaler — so a cause carried inside a chain entry survived as "{}" while the same error one level up rendered fine. A typed nil is the nil its producer meant and renders as null instead of panicking on the Error call; the normalization descends the map and slice shapes the package itself produces and leaves every other value to the encoder. */
func normalizeJsonValue(value any, remainingDepth int, seen map[jsonContextVisitKey]struct{}) any {
    if nil == value {
        return nil
    }

    if 0 >= remainingDepth {
        /* the floor bounds the DESCENT, not the scalar conversion: an error sitting at the floor still renders as its message, because handed to the encoder raw it marshals as the empty object — losing exactly the failure the record nested this deep to carry */
        if err, ok := value.(error); true == ok && false == internal.IsNilInterface(err) {
            if _, isMarshaler := value.(json.Marshaler); false == isMarshaler {
                return recoveredErrorMessage(err)
            }
        }

        /* a container is the one value the floor may not hand on as it is: nothing has walked what it holds, so a cycle closing below the bound would reach the encoder — and from there the fmt fallback that cannot survive one. A scalar carries no such risk and passes through. */
        if jsonContextContainerNone != plainJsonContextContainer(value).kind {
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

/* marshalJsonContextContained encodes the normalized context under a recover: the encoder calls the MarshalJSON and MarshalText of the caller's own values and hands back as a panic anything they raise that is not its own error, and a record is written from the recovery defers that report a failure — a second panic there went past the recovery reporting the first, and on a worker's goroutine with nothing above it that ended the process. A value whose encoding panicked answers an error naming the panic, and the record falls back to the text rendering it keeps for a context the encoder refuses. */
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

/* renderJsonContextByKey is the text a record carries when its context as a whole does not encode: each key is
   encoded on its own, so one value the encoder refuses costs that key alone and every other key of the record —
   the service name, the cause chain — keeps its encoded value. It is written as one json object in a string,
   the keys sorted, and a refused key reads as its reason.

   It does not hand the context to fmt: fmt has no cycle detection, and the normalization walks maps and slices
   but not structs, so a map held through a struct field that closes on itself reached fmt whole — and fmt
   recursed until the goroutine stack was gone, a fatal error no recover turns into a record. The encoder
   detects that cycle and refuses the value; the refusal is what the key then says. */
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
