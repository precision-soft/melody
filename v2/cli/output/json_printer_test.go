package output

import (
    "bytes"
    "encoding/json"
    "errors"
    "reflect"
    "strings"
    "testing"
)

type failingOutputWriter struct{}

func (instance failingOutputWriter) Write(payload []byte) (int, error) {
    return 0, errFailingOutputWriter
}

/* the json printer is the whole of what a script reads: the document has to be decodable, the table has to stay OUT of it — it is a rendering of the same data and duplicating it would double every listing a client parses — and a write failure has to be reported rather than swallowed, or a report truncated by a full disk would end with a success exit code. */
func TestJsonPrinter_WritesADecodableDocumentWithoutTheTableRendering(t *testing.T) {
    envelope := Envelope{
        Data:  map[string]any{"name": "melody"},
        Table: &TableData{Blocks: []TableBlock{{Title: "names", Columns: []string{"name"}, Rows: [][]string{{"melody"}}}}},
    }
    envelope.AddWarning("app.warning", "a warning", nil)

    buffer := &bytes.Buffer{}

    printErr := (&JsonPrinter{}).Print(buffer, envelope, DefaultOption())
    if nil != printErr {
        t.Fatalf("unexpected print error: %v", printErr)
    }

    decoded := map[string]any{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &decoded); nil != decodeErr {
        t.Fatalf("expected a decodable document, got %v for %q", decodeErr, buffer.String())
    }

    if _, leaked := decoded["Table"]; true == leaked {
        t.Fatalf("expected the table rendering to stay out of the json document, got %q", buffer.String())
    }

    data, isMap := decoded["data"].(map[string]any)
    if false == isMap || "melody" != data["name"] {
        t.Fatalf("expected the data to travel, got %#v", decoded["data"])
    }
}

/* a machine reads a stream of records, so one document is one line: a consumer following a long-running command hands each line to a parser whole. The document itself carries newlines nowhere else, which is what makes the framing safe. */
func TestJsonPrinter_TheMachineDocumentIsOneLine(t *testing.T) {
    envelope := Envelope{
        Data: map[string]any{"name": "melody", "nested": map[string]any{"deep": []any{1, 2, 3}}},
    }
    envelope.AddWarning("app.warning", "a warning", nil)

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Format = FormatJson

    printErr := (&JsonPrinter{}).Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("unexpected print error: %v", printErr)
    }

    rendered := buffer.String()

    if false == strings.HasSuffix(rendered, "\n") {
        t.Fatalf("expected the document to be terminated by a newline, got %q", rendered)
    }

    if 1 != strings.Count(rendered, "\n") {
        t.Fatalf("expected exactly one newline, the terminator, got %d in %q", strings.Count(rendered, "\n"), rendered)
    }

    decoded := map[string]any{}
    if decodeErr := json.Unmarshal([]byte(strings.TrimSuffix(rendered, "\n")), &decoded); nil != decodeErr {
        t.Fatalf("expected the single line to decode on its own, got %v for %q", decodeErr, rendered)
    }
}

func TestJsonPrinter_ThePrettyFormatIndentsTheSameDocument(t *testing.T) {
    envelope := Envelope{
        Data: map[string]any{"name": "melody"},
    }

    compactBuffer := &bytes.Buffer{}
    compactOption := DefaultOption()
    compactOption.Format = FormatJson

    if printErr := (&JsonPrinter{}).Print(compactBuffer, envelope, compactOption); nil != printErr {
        t.Fatalf("unexpected print error: %v", printErr)
    }

    prettyBuffer := &bytes.Buffer{}
    prettyOption := DefaultOption()
    prettyOption.Format = FormatJsonPretty

    if printErr := (&JsonPrinter{}).Print(prettyBuffer, envelope, prettyOption); nil != printErr {
        t.Fatalf("unexpected print error: %v", printErr)
    }

    if false == strings.Contains(prettyBuffer.String(), "\n  ") {
        t.Fatalf("expected the pretty document to be indented, got %q", prettyBuffer.String())
    }

    compactDecoded := map[string]any{}
    prettyDecoded := map[string]any{}

    if decodeErr := json.Unmarshal(compactBuffer.Bytes(), &compactDecoded); nil != decodeErr {
        t.Fatalf("unexpected compact decode error: %v", decodeErr)
    }
    if decodeErr := json.Unmarshal(prettyBuffer.Bytes(), &prettyDecoded); nil != decodeErr {
        t.Fatalf("unexpected pretty decode error: %v", decodeErr)
    }

    if false == reflect.DeepEqual(compactDecoded, prettyDecoded) {
        t.Fatalf("expected the two formats to differ in whitespace alone, got %#v and %#v", compactDecoded, prettyDecoded)
    }
}

func TestJsonPrinter_AWriteFailureIsReportedRatherThanSwallowed(t *testing.T) {
    printErr := (&JsonPrinter{}).Print(failingOutputWriter{}, Envelope{}, DefaultOption())

    if nil == printErr {
        t.Fatalf("expected the write failure to be reported")
    }
}

var errFailingOutputWriter = errors.New("the writer refused")
