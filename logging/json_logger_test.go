package logging

import (
    "bufio"
    "bytes"
    "encoding/json"
    "errors"
    "io"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func testNewJsonLogger() (loggingcontract.Logger, *bytes.Buffer) {
    buffer := &bytes.Buffer{}

    return NewJsonLogger(buffer, loggingcontract.LevelInfo), buffer
}

func testNewJsonLoggerWithMinLevel(minLevel loggingcontract.Level) (loggingcontract.Logger, *bytes.Buffer) {
    buffer := &bytes.Buffer{}

    return NewJsonLogger(buffer, minLevel), buffer
}

func TestJsonLogger_WritesJsonLine(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Info(
        "hello",
        map[string]any{
            "requestId": "r1",
        },
    )

    scanner := bufio.NewScanner(buffer)
    if false == scanner.Scan() {
        t.Fatalf("expected one log line")
    }

    var payload map[string]any
    err := json.Unmarshal(scanner.Bytes(), &payload)
    if nil != err {
        t.Fatalf("invalid json: %v", err)
    }

    message, ok := payload["message"].(string)
    if false == ok {
        t.Fatalf("missing message")
    }
    if "hello" != message {
        t.Fatalf("unexpected message: %s", message)
    }

    context, ok := payload["context"].(map[string]any)
    if false == ok {
        t.Fatalf("missing context")
    }

    if "r1" != context["requestId"] {
        t.Fatalf("unexpected context value")
    }
}

func TestJsonLogger_MinLevelFilters(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelError)

    logger.Info("info", nil)
    logger.Error("error", nil)

    scanner := bufio.NewScanner(buffer)

    if false == scanner.Scan() {
        t.Fatalf("expected at least one log line")
    }

    var payload map[string]any
    err := json.Unmarshal(scanner.Bytes(), &payload)
    if nil != err {
        t.Fatalf("invalid json: %v", err)
    }

    message, ok := payload["message"].(string)
    if false == ok {
        t.Fatalf("missing message")
    }
    if "error" != message {
        t.Fatalf("expected only error message")
    }

    if true == scanner.Scan() {
        t.Fatalf("expected only one line due to filter")
    }
}

func decodeJsonLine(t *testing.T, line string) map[string]any {
    t.Helper()

    var data map[string]any
    err := json.Unmarshal([]byte(line), &data)
    if nil != err {
        t.Fatalf("invalid json: %v", err)
    }
    return data
}

func TestJsonLogger_Info_WritesJsonWithMessageAndLevel(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelInfo)

    logger.Info("hello", nil)

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 1 != len(lines) {
        t.Fatalf("expected one log line")
    }

    data := decodeJsonLine(t, lines[0])

    if "info" != data["level"] {
        t.Fatalf("unexpected level")
    }
    if "hello" != data["message"] {
        t.Fatalf("unexpected message")
    }
}

func TestJsonLogger_LevelFiltering(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelWarning)

    logger.Info("info", nil)
    logger.Warning("warn", nil)
    logger.Error("error", nil)

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 2 != len(lines) {
        t.Fatalf("expected 2 log lines")
    }

    first := decodeJsonLine(t, lines[0])
    second := decodeJsonLine(t, lines[1])

    if "warning" != first["level"] {
        t.Fatalf("unexpected level")
    }
    if "error" != second["level"] {
        t.Fatalf("unexpected level")
    }
}

func TestNewJsonLogger_PanicsWhenOutputIsNil(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewJsonLogger(nil, loggingcontract.LevelInfo)
}

func TestNewJsonLogger_PanicsWhenLevelIsInvalid(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewJsonLogger(&bytes.Buffer{}, loggingcontract.Level("invalid"))
}

type testWriteCloser struct {
    buffer bytes.Buffer
    closed bool
}

func (instance *testWriteCloser) Write(data []byte) (int, error) {
    return instance.buffer.Write(data)
}

func (instance *testWriteCloser) Close() error {
    instance.closed = true
    return nil
}

func TestJsonLogger_EmptyContextDoesNotBreak(t *testing.T) {
    buffer := &bytes.Buffer{}
    logger := NewJsonLogger(buffer, loggingcontract.LevelInfo)

    logger.Info("msg", nil)

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 1 != len(lines) {
        t.Fatalf("expected one line")
    }

    data := decodeJsonLine(t, lines[0])

    if "msg" != data["message"] {
        t.Fatalf("unexpected message")
    }
}

func TestJsonLogger_NormalizesErrorToString(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelInfo)

    logger.Log(
        loggingcontract.LevelError,
        "test",
        map[string]any{
            "error": errors.New("boom"),
        },
    )

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 1 != len(lines) {
        t.Fatalf("expected one log line")
    }

    data := decodeJsonLine(t, lines[0])

    context, ok := data["context"].(map[string]any)
    if false == ok {
        t.Fatalf("missing context")
    }

    err, ok := context["error"].(string)
    if false == ok {
        t.Fatalf("expected err to be string")
    }

    if "boom" != err {
        t.Fatalf("unexpected err value: %s", err)
    }
}

func TestJsonLogger_CustomLevelLabels(t *testing.T) {
    buffer := &bytes.Buffer{}

    customLabels := loggingcontract.LevelLabels{
        loggingcontract.LevelDebug:     loggingcontract.LevelLabelFromInt(100),
        loggingcontract.LevelInfo:      loggingcontract.LevelLabelFromInt(200),
        loggingcontract.LevelWarning:   loggingcontract.LevelLabelFromInt(300),
        loggingcontract.LevelError:     loggingcontract.LevelLabelFromInt(400),
        loggingcontract.LevelEmergency: loggingcontract.LevelLabelFromInt(500),
    }

    logger := NewJsonLoggerWithLabels(buffer, loggingcontract.LevelDebug, customLabels)

    logger.Debug("d", nil)
    logger.Info("i", nil)
    logger.Warning("w", nil)
    logger.Error("e", nil)
    logger.Emergency("em", nil)

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 5 != len(lines) {
        t.Fatalf("expected 5 log lines, got %d", len(lines))
    }

    expected := []float64{100, 200, 300, 400, 500}
    for i, line := range lines {
        data := decodeJsonLine(t, line)
        level, ok := data["level"].(float64)
        if false == ok {
            t.Fatalf("line %d: level is not a number: %v", i, data["level"])
        }
        if expected[i] != level {
            t.Fatalf("line %d: expected level %v, got %v", i, expected[i], level)
        }
    }
}

func TestJsonLogger_ConcurrentWritesAreSerializedIntoCompleteLines(t *testing.T) {
    buffer := &bytes.Buffer{}
    logger := NewJsonLogger(buffer, loggingcontract.LevelInfo)

    var waitGroup sync.WaitGroup

    writerCount := 16
    messagesPerWriter := 100

    for writerIndex := 0; writerIndex < writerCount; writerIndex++ {
        waitGroup.Add(1)
        go func(writerId int) {
            defer waitGroup.Done()
            for iteration := 0; iteration < messagesPerWriter; iteration++ {
                logger.Info(
                    "concurrent write",
                    map[string]any{
                        "writerId":  writerId,
                        "iteration": iteration,
                    },
                )
            }
        }(writerIndex)
    }

    waitGroup.Wait()

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    expectedLines := writerCount * messagesPerWriter

    if expectedLines != len(lines) {
        t.Fatalf("expected %d log lines, got %d", expectedLines, len(lines))
    }

    for index, line := range lines {
        var payload map[string]any
        err := json.Unmarshal([]byte(line), &payload)
        if nil != err {
            t.Fatalf("line %d is not valid json: %v", index, err)
        }

        if "concurrent write" != payload["message"] {
            t.Fatalf("line %d: unexpected message: %v", index, payload["message"])
        }
    }
}

func TestJsonLogger_FallbackOnMarshalError(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelInfo)

    logger.Log(
        loggingcontract.LevelError,
        "test",
        map[string]any{
            "bad": make(chan int),
        },
    )

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 1 != len(lines) {
        t.Fatalf("expected one log line")
    }

    data := decodeJsonLine(t, lines[0])

    marshalErrorValue, ok := data["marshalError"].(string)
    if false == ok {
        t.Fatalf("expected marshalError in fallback payload")
    }

    if "" == marshalErrorValue {
        t.Fatalf("expected non-empty marshalError value")
    }

    message, ok := data["message"].(string)
    if false == ok {
        t.Fatalf("missing message")
    }
    if "test" != message {
        t.Fatalf("unexpected message: %s", message)
    }

    level, ok := data["level"].(string)
    if false == ok {
        t.Fatalf("missing level")
    }
    if "error" != level {
        t.Fatalf("unexpected level: %s", level)
    }

    timeValue, ok := data["time"].(string)
    if false == ok {
        t.Fatalf("missing time in fallback payload")
    }
    if _, parseErr := time.Parse(time.RFC3339, timeValue); nil != parseErr {
        t.Fatalf("fallback time is not valid RFC3339: %v", parseErr)
    }
}

type statefulProbeWriter struct {
    lines  []string
    closed bool
}

func (instance *statefulProbeWriter) Write(payload []byte) (int, error) {
    instance.lines = append(instance.lines, string(payload))

    return len(payload), nil
}

func (instance *statefulProbeWriter) Close() error {
    instance.closed = true

    return nil
}

/* Close hands the writer to a Close that may mutate it while a goroutine outliving the container teardown is still inside Write; run with -race */
func TestJsonLogger_CloseIsSynchronizedWithConcurrentWrites(t *testing.T) {
    writer := &statefulProbeWriter{}
    logger := NewJsonLogger(writer, loggingcontract.LevelDebug)

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 500; iteration++ {
            logger.Info("message", nil)
        }
    }()

    go func() {
        defer waitGroup.Done()

        closeable, isCloseable := logger.(interface{ Close() error })
        if false == isCloseable {
            return
        }

        _ = closeable.Close()
    }()

    waitGroup.Wait()
}

func TestJsonLogger_WritesAreDroppedAfterClose(t *testing.T) {
    writer := &statefulProbeWriter{}
    logger := NewJsonLogger(writer, loggingcontract.LevelDebug)

    logger.Info("before", nil)

    closeable, isCloseable := logger.(interface{ Close() error })
    if false == isCloseable {
        t.Fatalf("expected the logger to be closeable")
    }

    if closeErr := closeable.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    logger.Info("after", nil)

    if 1 != len(writer.lines) {
        t.Fatalf("expected only the pre-close line to be written, got %d", len(writer.lines))
    }

    if false == strings.Contains(writer.lines[0], "before") {
        t.Fatalf("unexpected written line: %s", writer.lines[0])
    }
}

/* Closed is what the process-boundary exit handler asks before trusting this logger with the final record: a file-backed logger closed by a teardown silently drops every write. */
func TestJsonLogger_ClosedReportsOnlyAReallyClosedWriter(t *testing.T) {
    file, createErr := os.CreateTemp(t.TempDir(), "melody-json-logger-*.log")
    if nil != createErr {
        t.Fatalf("unexpected temp file error: %v", createErr)
    }

    fileLogger := NewJsonLogger(file, loggingcontract.LevelInfo)

    closedChecker, isChecker := fileLogger.(interface{ Closed() bool })
    if false == isChecker {
        t.Fatalf("expected the json logger to report closedness")
    }

    if true == closedChecker.Closed() {
        t.Fatalf("expected an open logger to report not closed")
    }

    closer, isCloser := fileLogger.(interface{ Close() error })
    if false == isCloser {
        t.Fatalf("expected the json logger to be closable")
    }

    if closeErr := closer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.Closed() {
        t.Fatalf("expected a closed file-backed logger to report closed")
    }

    consoleLogger := NewJsonLogger(os.Stderr, loggingcontract.LevelInfo)

    consoleCloser := consoleLogger.(interface{ Close() error })
    if closeErr := consoleCloser.Close(); nil != closeErr {
        t.Fatalf("unexpected console close error: %v", closeErr)
    }

    consoleChecker := consoleLogger.(interface{ Closed() bool })
    if true == consoleChecker.Closed() {
        t.Fatalf("expected the console logger to stay open and report not closed")
    }
}

/* a typed nil stored under a context key matched the error assertion and Error() dereferenced the nil receiver inside Log — a panic on the logging path, reachable from the exit handler; it renders as null, the nil its producer meant */
func TestJsonLogger_NormalizesTypedNilErrorToNull(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Error(
        "record",
        map[string]any{
            "cause": (*typedNilProbeError)(nil),
        },
    )

    data := decodeJsonLine(t, strings.TrimSpace(buffer.String()))

    context, isMap := data["context"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected context in the record")
    }

    causeValue, hasCause := context["cause"]
    if false == hasCause || nil != causeValue {
        t.Fatalf("expected the typed-nil cause to render as null, got %v", causeValue)
    }
}

/* an error one level down used to reach the encoder unnormalized and marshal as an empty object — every field unexported, no marshaler — so the diagnostic survived at the top level and vanished one level below it */
func TestJsonLogger_NormalizesNestedErrors(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Error(
        "record",
        map[string]any{
            "details": map[string]any{
                "dbErr": errors.New("no rows"),
            },
            "chain": []map[string]any{
                {"linkErr": errors.New("link failed")},
            },
            "values": []any{errors.New("element failed")},
        },
    )

    line := strings.TrimSpace(buffer.String())
    data := decodeJsonLine(t, line)

    context, isMap := data["context"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected context in the record")
    }

    details, isDetails := context["details"].(map[string]any)
    if false == isDetails || "no rows" != details["dbErr"] {
        t.Fatalf("expected the nested error message, got %v", context["details"])
    }

    if true == strings.Contains(line, "{}") {
        t.Fatalf("expected no empty-object rendering of an error, got %s", line)
    }
}

/* one unmarshalable value used to cost every other key of the record: the fallback now carries the whole context as text, so the service name and the cause survive next to the marshal error */
func TestJsonLogger_FallbackKeepsTheContextAsText(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelInfo)

    logger.Error(
        "record",
        map[string]any{
            "bad":     make(chan int),
            "service": "the-culprit",
        },
    )

    data := decodeJsonLine(t, strings.TrimSpace(buffer.String()))

    contextText, isString := data["context"].(string)
    if false == isString {
        t.Fatalf("expected the fallback to carry the context as text, got %T", data["context"])
    }

    if false == strings.Contains(contextText, "the-culprit") {
        t.Fatalf("expected the surviving keys in the fallback context, got %s", contextText)
    }
}

type gatedProbeWriter struct {
    entered chan struct{}
    release chan struct{}
}

func (instance *gatedProbeWriter) Write(payload []byte) (int, error) {
    close(instance.entered)
    <-instance.release

    return len(payload), nil
}

/* Closed is asked by the process-boundary exit handler; an answer serialized behind an in-flight Write into a stalled pipe used to hang the one handler that must reach os.Exit — the probe is held open inside Write while Closed is required to answer */
func TestJsonLogger_ClosedAnswersWhileAWriteIsInFlight(t *testing.T) {
    writer := &gatedProbeWriter{
        entered: make(chan struct{}),
        release: make(chan struct{}),
    }

    logger := NewJsonLogger(writer, loggingcontract.LevelInfo)

    go func() {
        logger.Info("stalled record", nil)
    }()

    <-writer.entered

    answered := make(chan bool, 1)
    go func() {
        closedChecker := logger.(interface{ Closed() bool })
        answered <- closedChecker.Closed()
    }()

    select {
    case isClosed := <-answered:
        if true == isClosed {
            t.Fatalf("expected the stalled logger to report open")
        }

    case <-time.After(2 * time.Second):
        t.Fatalf("expected Closed to answer while the write is in flight")
    }

    close(writer.release)
}

/* the console is recognized by identity — the os.Stdout and os.Stderr values themselves — not by name: a file the caller opened on the "/dev/stdout" path is a descriptor this logger owns, and the name check skipped the close it owed and leaked it once per boot */
func TestJsonLogger_CloseClosesAFileOpenedOnTheConsolePath(t *testing.T) {
    file, openErr := os.OpenFile("/dev/stdout", os.O_WRONLY|os.O_APPEND, 0644)
    if nil != openErr {
        t.Skipf("cannot open /dev/stdout in this environment: %v", openErr)
    }

    logger := NewJsonLogger(file, loggingcontract.LevelInfo)

    closer := logger.(interface{ Close() error })
    if closeErr := closer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    closedChecker := logger.(interface{ Closed() bool })
    if false == closedChecker.Closed() {
        t.Fatalf("expected the opened descriptor to be really closed")
    }
}

/* the labels are read lock-free on every Log call while the caller keeps its reference: without the copy, a later write into that map raced the reads fatally; with it, the logger keeps rendering the labels it was built with */
func TestNewJsonLoggerWithLabels_CopiesTheLabels(t *testing.T) {
    labels := loggingcontract.LevelLabels{
        loggingcontract.LevelError: loggingcontract.LevelLabelFromString("custom-error"),
    }

    buffer := &bytes.Buffer{}
    logger := NewJsonLoggerWithLabels(buffer, loggingcontract.LevelInfo, labels)

    labels[loggingcontract.LevelError] = loggingcontract.LevelLabelFromString("mutated-error")

    logger.Error("record", nil)

    if false == strings.Contains(buffer.String(), "custom-error") {
        t.Fatalf("expected the label the logger was built with, got %s", buffer.String())
    }
}

type marshalingProbeError struct {
    detail string
}

func (instance *marshalingProbeError) Error() string {
    return "flattened"
}

func (instance *marshalingProbeError) MarshalJSON() ([]byte, error) {
    return json.Marshal(map[string]string{"detail": instance.detail})
}

/* an error that also marshals itself opts into a structural rendering — the validation error collection says the same thing in the log that it says in the response body — while a plain error still renders as its message */
func TestJsonLogger_ErrorImplementingMarshalerRendersStructurally(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Error(
        "record",
        map[string]any{
            "structured": &marshalingProbeError{detail: "field level"},
            "plain":      errors.New("plain failure"),
        },
    )

    data := decodeJsonLine(t, strings.TrimSpace(buffer.String()))

    context, isMap := data["context"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected context in the record")
    }

    structured, isStructured := context["structured"].(map[string]any)
    if false == isStructured || "field level" != structured["detail"] {
        t.Fatalf("expected the marshaler error rendered structurally, got %v", context["structured"])
    }

    if "plain failure" != context["plain"] {
        t.Fatalf("expected the plain error rendered as its message, got %v", context["plain"])
    }
}

/* a level outside the five known ones weighs as error instead of debug — the unknown level is the case that least deserves silence, and the zero-value exception carries an empty one; the label keeps the raw level so the record says what it was handed */
func TestJsonLogger_UnknownLevelIsWeighedAsError(t *testing.T) {
    logger, buffer := testNewJsonLoggerWithMinLevel(loggingcontract.LevelInfo)

    logger.Log(loggingcontract.Level(""), "empty level record", nil)
    logger.Log(loggingcontract.LevelUnknown, "unknown level record", nil)

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
    if 2 != len(lines) {
        t.Fatalf("expected both unknown-level records to survive the threshold, got %d lines: %s", len(lines), buffer.String())
    }

    firstRecord := decodeJsonLine(t, lines[0])
    if "" != firstRecord["level"] {
        t.Fatalf("expected the raw empty level in the record, got %v", firstRecord["level"])
    }

    secondRecord := decodeJsonLine(t, lines[1])
    if "unknown" != secondRecord["level"] {
        t.Fatalf("expected the raw unknown level in the record, got %v", secondRecord["level"])
    }
}

/* nanosecond precision keeps the ordering the write mutex pays for; three records make an all-zero fraction astronomically unlikely, and the stamp still parses under the RFC 3339 layout */
func TestJsonLogger_TimestampCarriesSubSecondPrecision(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Info("first", nil)
    logger.Info("second", nil)
    logger.Info("third", nil)

    lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")

    fractionSeen := false
    for _, line := range lines {
        data := decodeJsonLine(t, line)

        timeValue, isString := data["time"].(string)
        if false == isString {
            t.Fatalf("expected a time value")
        }

        if _, parseErr := time.Parse(time.RFC3339Nano, timeValue); nil != parseErr {
            t.Fatalf("expected an RFC 3339 parsable stamp, got %q: %v", timeValue, parseErr)
        }

        if true == strings.Contains(timeValue, ".") {
            fractionSeen = true
        }
    }

    if false == fractionSeen {
        t.Fatalf("expected at least one stamp with a sub-second fraction, got %s", buffer.String())
    }
}

/* Close is called by the container teardown and can be called again by an owner that also holds the logger; the second call must not hand the writer to Close twice — a file descriptor closed twice is a descriptor another goroutine may already have been given by the operating system */
func TestJsonLogger_Close_IsIdempotent(t *testing.T) {
    file, createErr := os.CreateTemp(t.TempDir(), "melody-json-logger-*.log")
    if nil != createErr {
        t.Fatalf("unexpected temp file error: %v", createErr)
    }

    logger := NewJsonLogger(file, loggingcontract.LevelInfo)

    closer := logger.(interface{ Close() error })

    if closeErr := closer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := closer.Close(); nil != closeErr {
        t.Fatalf("expected the second close to be a no-op, got %v", closeErr)
    }

    if false == logger.(interface{ Closed() bool }).Closed() {
        t.Fatalf("expected the logger to report itself closed")
    }
}

/* a writer that cannot be closed — a buffer, a pipe half held by somebody else — is left alone and the logger keeps writing: reporting itself closed would make the exit handler refuse a logger that is perfectly alive, and the final record would be routed to the emergency logger for nothing */
func TestJsonLogger_Close_WithANonClosableWriter_KeepsTheLoggerAlive(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    closer := logger.(interface{ Close() error })

    if closeErr := closer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == logger.(interface{ Closed() bool }).Closed() {
        t.Fatalf("expected a logger over a non-closable writer to stay open")
    }

    logger.Error("after the close", nil)

    if false == strings.Contains(buffer.String(), "after the close") {
        t.Fatalf("expected the record to still be written, got %q", buffer.String())
    }
}

/* the normalization descends into the context maps the framework itself nests — the cause context chain is exactly this shape — and the exception contract's Context is the type those maps arrive as; without the case for it, an error one level down inside one would reach the encoder unconverted and marshal as an empty object */
func TestJsonLogger_NestedContextTypedMap_IsNormalizedLikeAPlainMap(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Error(
        "message",
        loggingcontract.Context{
            "nested": loggingcontract.Context{
                "cause": errors.New("the nested cause"),
            },
        },
    )

    record := map[string]any{}
    if unmarshalErr := json.Unmarshal(buffer.Bytes(), &record); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v for %q", unmarshalErr, buffer.String())
    }

    context, isMap := record["context"].(map[string]any)
    if false == isMap {
        t.Fatalf("unexpected context shape: %v", record["context"])
    }

    nested, isNestedMap := context["nested"].(map[string]any)
    if false == isNestedMap {
        t.Fatalf("expected the nested context to be rendered as an object, got %v", context["nested"])
    }

    if "the nested cause" != nested["cause"] {
        t.Fatalf("expected the nested error to render as its message, got %v", nested["cause"])
    }
}

/* a nil context value renders as json null rather than being dropped or reaching the encoder as an untyped nil inside a converted container */
func TestJsonLogger_NilContextValue_RendersAsNull(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Error("message", loggingcontract.Context{"missing": nil})

    if false == strings.Contains(buffer.String(), `"missing":null`) {
        t.Fatalf("expected a null value, got %q", buffer.String())
    }
}

/* the descent is bounded: a context that nests deeper than the bound is handed to the encoder as it is instead of recursing without end, which is what keeps a pathological self-referencing structure from taking the process down inside the logger. The assertion is written at the boundary itself — one level above it the error still renders as its message, one level below it reaches the encoder unconverted and marshals as an empty object — because a bound asserted from far away survives being moved by one */
func TestJsonLogger_ContextDepthBound_IsAppliedExactlyWhereItIsDeclared(t *testing.T) {
    buildNestedContext := func(depth int) loggingcontract.Context {
        nested := any(map[string]any{"cause": errors.New("at the bound")})

        for level := 0; level < depth; level++ {
            nested = map[string]any{"level": nested}
        }

        return loggingcontract.Context{"root": nested}
    }

    /* the context map itself is the first level the walk descends, so the last level it still converts sits two below the bound */
    logger, buffer := testNewJsonLogger()
    logger.Error("message", buildNestedContext(normalizeJsonContextMaxDepth-2))

    if false == strings.Contains(buffer.String(), `"cause":"at the bound"`) {
        t.Fatalf("expected the error just above the bound to render as its message, got %q", buffer.String())
    }

    logger, buffer = testNewJsonLogger()
    logger.Error("message", buildNestedContext(normalizeJsonContextMaxDepth-1))

    if false == strings.Contains(buffer.String(), `"cause":{}`) {
        t.Fatalf("expected the error below the bound to reach the encoder unconverted, got %q", buffer.String())
    }
}

type failingJsonLogWriter struct {
    writeCount int
}

func (instance *failingJsonLogWriter) Write(payload []byte) (int, error) {
    instance.writeCount = instance.writeCount + 1

    return 0, errors.New("no space left on device")
}

func TestJsonLogger_TheFirstFailedWriteIsEchoedOnStderrExactlyOnce(t *testing.T) {
    originalStderr := os.Stderr

    reader, writer, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }

    os.Stderr = writer

    output := &failingJsonLogWriter{}
    logger := NewJsonLogger(output, loggingcontract.LevelInfo)

    logger.Info("one", nil)
    logger.Info("two", nil)
    logger.Error("three", nil)

    writer.Close()
    os.Stderr = originalStderr

    echoed, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    if 3 != output.writeCount {
        t.Fatalf("expected the logger to keep trying its own output, got %d writes", output.writeCount)
    }

    if false == strings.Contains(string(echoed), "no space left on device") {
        t.Fatalf("expected the silenced journal to be reported on stderr, got %q", string(echoed))
    }

    if 1 != strings.Count(string(echoed), "melody: the json logger failed to write") {
        t.Fatalf("expected exactly one echo for a logger that fails on every record, got %q", string(echoed))
    }
}

func TestJsonLogger_TheEchoIsSkippedWhenTheOutputIsStderrItself(t *testing.T) {
    originalStderr := os.Stderr

    reader, writer, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }

    os.Stderr = writer

    logger := &jsonLogger{output: os.Stderr}
    logger.reportWriteFailure(errors.New("no space left on device"))

    writer.Close()
    os.Stderr = originalStderr

    echoed, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    if 0 != len(echoed) {
        t.Fatalf("expected no second attempt at the destination that just refused the first, got %q", string(echoed))
    }
}

/* blockingOrderedWriter holds the first write open until it is released, so the interleaving the guard is about is CONSTRUCTED rather than waited for */
type blockingOrderedWriter struct {
    mutex        sync.Mutex
    written      [][]byte
    firstWriteAt chan struct{}
    release      chan struct{}
    blocked      bool
}

func (instance *blockingOrderedWriter) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    copied := make([]byte, len(payload))
    copy(copied, payload)
    instance.written = append(instance.written, copied)
    shouldBlock := false == instance.blocked
    instance.blocked = true
    instance.mutex.Unlock()

    if true == shouldBlock {
        close(instance.firstWriteAt)
        <-instance.release
    }

    return len(payload), nil
}

/*
TestJsonLogger_TheStampOrderIsTheWriteOrder pins a guard against a RACE, so it
is proven by construction rather than by a mutant: one write is held open while
a second record is asked for, and the second record's stamp cannot precede the
first write's completion unless the stamp is taken outside the lock. It was:
the stamp said when the record was FORMED and the encoding happened between the
stamp and the write, so at eight goroutines 484 records of 1600 reached the
file out of stamp order while LOGGING.md promised the write order stays
reconstructible from them.
*/
func TestJsonLogger_TheStampOrderIsTheWriteOrder(t *testing.T) {
    writer := &blockingOrderedWriter{
        firstWriteAt: make(chan struct{}),
        release:      make(chan struct{}),
    }

    logger := NewJsonLogger(writer, loggingcontract.LevelDebug)

    firstDone := make(chan struct{})
    go func() {
        defer close(firstDone)

        logger.Info("first", nil)
    }()

    <-writer.firstWriteAt

    secondDone := make(chan struct{})
    go func() {
        defer close(secondDone)

        logger.Info("second", nil)
    }()

    /* the first write is held open across a real interval: whatever the scheduler does with the second goroutine, its stamp can only land later than this, so the assertion below never depends on scheduling luck */
    heldOpen := 150 * time.Millisecond
    time.Sleep(heldOpen)

    close(writer.release)

    <-firstDone
    <-secondDone

    writer.mutex.Lock()
    written := writer.written
    writer.mutex.Unlock()

    if 2 != len(written) {
        t.Fatalf("expected both records to be written, got %d", len(written))
    }

    firstStamp := stampOfRecord(t, written[0])
    secondStamp := stampOfRecord(t, written[1])

    if false == secondStamp.After(firstStamp) {
        t.Fatalf("expected the second record to be stamped after the first, got %v and %v", firstStamp, secondStamp)
    }

    if secondStamp.Sub(firstStamp) < heldOpen/2 {
        t.Fatalf(
            "expected the second stamp to be taken after the first write completed, got a gap of %v while the write was held open for %v",
            secondStamp.Sub(firstStamp),
            heldOpen,
        )
    }
}

func stampOfRecord(t *testing.T, payload []byte) time.Time {
    t.Helper()

    entry := map[string]any{}
    if unmarshalErr := json.Unmarshal(bytes.TrimSpace(payload), &entry); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v", unmarshalErr)
    }

    stamp, isString := entry["time"].(string)
    if false == isString {
        t.Fatalf("expected a stamp on the record, got %v", entry["time"])
    }

    parsed, parseErr := time.Parse(time.RFC3339Nano, stamp)
    if nil != parseErr {
        t.Fatalf("unexpected stamp format: %v", parseErr)
    }

    return parsed
}
