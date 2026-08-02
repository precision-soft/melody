package output

import (
    "bytes"
    "encoding/json"
    "errors"
    "strings"
    "testing"
)

type failingOutputWriter struct{}

func (instance failingOutputWriter) Write(payload []byte) (int, error) {
    return 0, errFailingOutputWriter
}

/* @info the json printer is the whole of what a script reads, and nothing had ever driven it: the document has to be decodable — an indentation change is cosmetic, a malformed document is not — the table has to stay OUT of it, because it is a rendering of the same data and duplicating it would double every listing a client parses, and a write failure has to be reported rather than swallowed, or a report truncated by a full disk would end with a success exit code. */
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

    if false == strings.Contains(buffer.String(), "\n  ") {
        t.Fatalf("expected the document to be indented for a human reading it by hand, got %q", buffer.String())
    }

    data, isMap := decoded["data"].(map[string]any)
    if false == isMap || "melody" != data["name"] {
        t.Fatalf("expected the data to travel, got %#v", decoded["data"])
    }
}

func TestJsonPrinter_AWriteFailureIsReportedRatherThanSwallowed(t *testing.T) {
    printErr := (&JsonPrinter{}).Print(failingOutputWriter{}, Envelope{}, DefaultOption())

    if nil == printErr {
        t.Fatalf("expected the write failure to be reported")
    }
}

var errFailingOutputWriter = errors.New("the writer refused")
