package logging

import (
    "bufio"
    "bytes"
    "encoding/json"
    "errors"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

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

/* @info Close hands the writer to a Close that may mutate it while a goroutine outliving the container teardown is still inside Write; run with -race */
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

