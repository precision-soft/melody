package logging

import (
    "bufio"
    "bytes"
    "fmt"
    "encoding/json"
    "errors"
    "io"
    "os"
    "sort"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func testNewJsonLogger() (loggingcontract.Logger, *bytes.Buffer) {
    buffer := &bytes.Buffer{}

    return NewJsonLogger(buffer, loggingcontract.LevelInfo), buffer
}

func testNewJsonLoggerWithMinLevel(minLevel loggingcontract.Level) (loggingcontract.Logger, *bytes.Buffer) {
    buffer := &bytes.Buffer{}

    return NewJsonLogger(buffer, minLevel), buffer
}

func testEncodedJsonString(t *testing.T, value string) string {
    t.Helper()

    encoded, err := json.Marshal(value)
    if nil != err {
        t.Fatalf("could not encode %q: %v", value, err)
    }

    return string(encoded)
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

func TestJsonLogger_NilContextValue_RendersAsNull(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Error("message", loggingcontract.Context{"missing": nil})

    if false == strings.Contains(buffer.String(), `"missing":null`) {
        t.Fatalf("expected a null value, got %q", buffer.String())
    }
}

func TestJsonLogger_ContextDepthBound_IsAppliedExactlyWhereItIsDeclared(t *testing.T) {
    buildNestedContext := func(depth int) loggingcontract.Context {
        nested := any(map[string]any{"cause": errors.New("at the bound")})

        for level := 0; level < depth; level++ {
            nested = map[string]any{"level": nested}
        }

        return loggingcontract.Context{"root": nested}
    }

    logger, buffer := testNewJsonLogger()
    logger.Error("message", buildNestedContext(normalizeJsonContextMaxDepth-2))

    if false == strings.Contains(buffer.String(), `"cause":"at the bound"`) {
        t.Fatalf("expected the error just above the bound to render as its message, got %q", buffer.String())
    }

    logger, buffer = testNewJsonLogger()
    logger.Error("message", buildNestedContext(normalizeJsonContextMaxDepth-1))

    if false == strings.Contains(buffer.String(), `"cause":"at the bound"`) {
        t.Fatalf("expected the error at the floor to render as its message, got %q", buffer.String())
    }

    logger, buffer = testNewJsonLogger()
    logger.Error("message", buildNestedContext(normalizeJsonContextMaxDepth))

    if false == strings.Contains(buffer.String(), `"level":`+testEncodedJsonString(t, normalizeJsonContextDepthMarker)) {
        t.Fatalf("expected the container below the bound to be replaced by the depth marker, got %q", buffer.String())
    }

    if true == strings.Contains(buffer.String(), `"cause":{}`) {
        t.Fatalf("expected no unwalked container to reach the encoder, got %q", buffer.String())
    }
}

type testForeignContext map[string]any

type testForeignList []any

func TestJsonLogger_ACyclicContextRendersTheCycleMarkerInsteadOfTakingTheProcessDown(t *testing.T) {
    selfNamingMap := map[string]any{"name": "outer"}
    selfNamingMap["self"] = selfNamingMap

    selfNamingContext := loggingcontract.Context{"name": "typed"}
    selfNamingContext["self"] = selfNamingContext

    selfNamingSlice := make([]any, 2)
    selfNamingSlice[0] = "first"
    selfNamingSlice[1] = selfNamingSlice

    selfNamingForeignMap := testForeignContext{"name": "foreign"}
    selfNamingForeignMap["self"] = selfNamingForeignMap

    selfNamingForeignSlice := make(testForeignList, 2)
    selfNamingForeignSlice[0] = "first"
    selfNamingForeignSlice[1] = selfNamingForeignSlice

    testCases := []struct {
        name  string
        value any
    }{
        {name: "a plain map holding itself", value: selfNamingMap},
        {name: "a defined context type holding itself", value: selfNamingContext},
        {name: "a slice holding itself", value: selfNamingSlice},
        {name: "a foreign defined map type holding itself", value: selfNamingForeignMap},
        {name: "a foreign defined slice type holding itself", value: selfNamingForeignSlice},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            logger, buffer := testNewJsonLogger()
            logger.Error("failure", loggingcontract.Context{"detail": testCase.value})

            rendered := buffer.String()
            if false == strings.Contains(rendered, testEncodedJsonString(t, normalizeJsonContextCycleMarker)) {
                t.Fatalf("expected the cycle to render as the marker, got %q", rendered)
            }

            if false == strings.Contains(rendered, `"message":"failure"`) {
                t.Fatalf("expected the record to keep its message, got %q", rendered)
            }

            if true == strings.Contains(rendered, "marshalError") {
                t.Fatalf("expected the encoder to succeed rather than fall back, got %q", rendered)
            }
        })
    }
}

func TestJsonLogger_AContextNamingOneMapTwiceRendersItBothTimes(t *testing.T) {
    shared := map[string]any{"shared": "value"}

    logger, buffer := testNewJsonLogger()
    logger.Error("message", loggingcontract.Context{"first": shared, "second": shared})

    rendered := buffer.String()
    if true == strings.Contains(rendered, testEncodedJsonString(t, normalizeJsonContextCycleMarker)) {
        t.Fatalf("expected two sibling references to render whole, got %q", rendered)
    }

    if 2 != strings.Count(rendered, `"shared":"value"`) {
        t.Fatalf("expected the shared map to render under both keys, got %q", rendered)
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

func TestJsonLogger_EnabledAnswersTheThresholdLogItselfApplies(t *testing.T) {
    logger := NewJsonLogger(io.Discard, loggingcontract.LevelWarning)

    levelReporter, isReporter := logger.(loggingcontract.LevelReporter)
    if false == isReporter {
        t.Fatalf("expected the json logger to answer the level question")
    }

    cases := []struct {
        level    loggingcontract.Level
        expected bool
    }{
        {loggingcontract.LevelDebug, false},
        {loggingcontract.LevelInfo, false},
        {loggingcontract.LevelWarning, true},
        {loggingcontract.LevelError, true},
        {loggingcontract.LevelEmergency, true},
        {loggingcontract.Level("audit"), true},
    }

    for _, testCase := range cases {
        if testCase.expected != levelReporter.Enabled(testCase.level) {
            t.Fatalf("expected %q enabled=%v under a warning threshold", testCase.level, testCase.expected)
        }
    }
}

func TestJsonLogger_EnabledReportsNothingOnceClosed(t *testing.T) {
    file, createErr := os.CreateTemp(t.TempDir(), "melody-json-logger-enabled-*.log")
    if nil != createErr {
        t.Fatalf("unexpected temp file error: %v", createErr)
    }

    logger := NewJsonLogger(file, loggingcontract.LevelDebug)

    levelReporter := logger.(loggingcontract.LevelReporter)

    if false == levelReporter.Enabled(loggingcontract.LevelDebug) {
        t.Fatalf("expected debug to be enabled before the logger is closed")
    }

    if closeErr := logger.(interface{ Close() error }).Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == levelReporter.Enabled(loggingcontract.LevelEmergency) {
        t.Fatalf("expected a closed logger to report nothing enabled")
    }
}

func TestNewJsonLoggerWithClock_StampsTheRecordFromTheInjectedClock(t *testing.T) {
    frozenInstant := time.Date(2026, time.August, 26, 10, 30, 0, 123456789, time.UTC)

    buffer := &bytes.Buffer{}
    logger := NewJsonLoggerWithClock(
        buffer,
        loggingcontract.LevelInfo,
        loggingcontract.DefaultLevelLabels(),
        clock.NewFrozenClock(frozenInstant),
    )

    logger.Info("stamped", nil)

    var payload map[string]any
    if unmarshalErr := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &payload); nil != unmarshalErr {
        t.Fatalf("invalid json: %v", unmarshalErr)
    }

    stamp, ok := payload["time"].(string)
    if false == ok {
        t.Fatalf("missing time")
    }
    if frozenInstant.Format(jsonLogTimestampLayout) != stamp {
        t.Fatalf("expected the frozen instant %s, got %s", frozenInstant.Format(jsonLogTimestampLayout), stamp)
    }
}

func TestNewJsonLoggerWithClock_StampsAreFixedWidthSoTheirTextOrderIsTheWriteOrder(t *testing.T) {
    instants := []time.Time{
        time.Date(2026, time.August, 26, 10, 30, 0, 0, time.UTC),
        time.Date(2026, time.August, 26, 10, 30, 0, 500000000, time.UTC),
        time.Date(2026, time.August, 26, 13, 30, 0, 750000000, time.FixedZone("east", 3*60*60)),
        time.Date(2026, time.August, 26, 10, 30, 1, 0, time.UTC),
    }

    stamps := []string{}
    for _, instant := range instants {
        buffer := &bytes.Buffer{}
        logger := NewJsonLoggerWithClock(
            buffer,
            loggingcontract.LevelInfo,
            loggingcontract.DefaultLevelLabels(),
            clock.NewFrozenClock(instant),
        )

        logger.Info("stamped", nil)

        var payload map[string]any
        if unmarshalErr := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &payload); nil != unmarshalErr {
            t.Fatalf("invalid json: %v", unmarshalErr)
        }

        stamp, ok := payload["time"].(string)
        if false == ok {
            t.Fatalf("missing time")
        }

        stamps = append(stamps, stamp)
    }

    sorted := append([]string{}, stamps...)
    sort.Strings(sorted)

    for index := range stamps {
        if stamps[index] != sorted[index] {
            t.Fatalf("the stamps do not sort into the order they were written: written %v, sorted %v", stamps, sorted)
        }
    }
}

func TestNewJsonLoggerWithClock_RefusesANilClock(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the nil clock to be refused")
        }
        if false == strings.Contains(fmt.Sprintf("%v", recoveredValue), "json logger clock is not provided") {
            t.Fatalf("expected the refusal to name the clock, got %v", recoveredValue)
        }
    }()

    _ = NewJsonLoggerWithClock(
        &bytes.Buffer{},
        loggingcontract.LevelInfo,
        loggingcontract.DefaultLevelLabels(),
        nil,
    )
}

func TestJsonLogger_AContextValueThatLogsWhileItRendersDoesNotDeadlockTheLogger(t *testing.T) {
    buffer := &bytes.Buffer{}
    logger := NewJsonLogger(buffer, loggingcontract.LevelInfo)

    done := make(chan struct{})

    go func() {
        defer close(done)

        logger.Info("outer", loggingcontract.Context{"value": &loggingReentrantValue{logger: logger}})
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatalf("the logger deadlocked on a context value that logs while it renders")
    }

    if false == strings.Contains(buffer.String(), "from inside the marshaller") {
        t.Fatalf("expected the re-entrant record, got %q", buffer.String())
    }

    if false == strings.Contains(buffer.String(), "outer") {
        t.Fatalf("expected the outer record, got %q", buffer.String())
    }
}

type loggingReentrantValue struct {
    logger loggingcontract.Logger
}

func (instance *loggingReentrantValue) MarshalJSON() ([]byte, error) {
    instance.logger.Info("from inside the marshaller", nil)

    return []byte(`"rendered"`), nil
}

func TestJsonLogger_AStalledStderrEchoDoesNotHoldTheWriteLock(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd

    filled := make(chan struct{})
    echoed := make(chan struct{})

    defer func() {
        _ = readEnd.Close()

        <-filled
        <-echoed

        os.Stderr = originalStderr
        _ = writeEnd.Close()
    }()

    go func() {
        defer close(filled)

        _, _ = writeEnd.Write(make([]byte, 1<<20))
    }()

    time.Sleep(50 * time.Millisecond)

    logger := NewJsonLogger(&refusingWriter{}, loggingcontract.LevelInfo)

    go func() {
        defer close(echoed)

        logger.Info("the record whose write fails", nil)
    }()

    time.Sleep(50 * time.Millisecond)

    closer, isCloser := logger.(io.Closer)
    if false == isCloser {
        t.Fatalf("expected the json logger to be closeable")
    }

    closed := make(chan struct{})
    go func() {
        defer close(closed)

        _ = closer.Close()
    }()

    select {
    case <-closed:
    case <-time.After(5 * time.Second):
        t.Fatalf("Close waited on the write lock while the stderr echo was stalled")
    }
}

type refusingWriter struct {
}

func (instance *refusingWriter) Write(payload []byte) (int, error) {
    return 0, errors.New("the journal destination refuses every record")
}


func TestJsonLogger_SpellsTheC1BlockAsJsonEscapes(t *testing.T) {
    logger, buffer := testNewJsonLogger()

    logger.Info("hi\xc2\x9bthere", map[string]any{"k": "v\xc2\x85w"})

    line := buffer.Bytes()

    for _, continuation := range []byte{0x85, 0x9b} {
        if true == bytes.Contains(line, []byte{0xc2, continuation}) {
            t.Fatalf("a raw C1 rune c2 %02x survived in %q", continuation, line)
        }

        spelling := "\\" + "u00" + fmt.Sprintf("%02x", continuation)
        if false == bytes.Contains(line, []byte(spelling)) {
            t.Fatalf("expected the spelling %s in %q", spelling, line)
        }
    }

    var payload map[string]any
    if decodeErr := json.Unmarshal(line, &payload); nil != decodeErr {
        t.Fatalf("invalid json: %v for %q", decodeErr, line)
    }

    if "hi\xc2\x9bthere" != payload["message"] {
        t.Fatalf("expected the message decoded to the value given, got %#v", payload["message"])
    }

    context, isMap := payload["context"].(map[string]any)
    if false == isMap || "v\xc2\x85w" != context["k"] {
        t.Fatalf("expected the context decoded to the value given, got %#v", payload["context"])
    }
}
