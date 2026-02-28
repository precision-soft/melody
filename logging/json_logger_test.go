package logging

import (
    "bufio"
    "bytes"
    "encoding/json"
    "errors"
    "strings"
    "testing"

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
}
