package output

import (
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
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

/* the encoder emits the C1 block raw, so a document carrying U+009B repainted the terminal it was printed to and a NEL ended the record for a reader splitting on Unicode line boundaries; the printer spells the block as json escapes on the way out — in the data, in a key, in a warning, in the one-line and in the pretty form alike — and the decoded document is the one the command gave. The spelling is asked of the encoder's own vocabulary rather than typed, so the assertion follows the encoder if it ever changes. */
func TestJsonPrinter_SpellsTheC1BlockAsJsonEscapes(t *testing.T) {
    for _, format := range []Format{FormatJson, FormatJsonPretty} {
        envelope := Envelope{Data: map[string]any{"name": "a\xc2\x9bb", "k\xc2\x9dey": "value"}}
        envelope.AddWarning("app.warning", "wa\xc2\x85rn", nil)

        option := DefaultOption()
        option.Format = format

        buffer := &bytes.Buffer{}

        if printErr := (&JsonPrinter{}).Print(buffer, envelope, option); nil != printErr {
            t.Fatalf("%s: unexpected print error: %v", format, printErr)
        }

        for _, continuation := range []byte{0x85, 0x9b, 0x9d} {
            if true == bytes.Contains(buffer.Bytes(), []byte{0xc2, continuation}) {
                t.Fatalf("%s: a raw C1 rune c2 %02x survived in %q", format, continuation, buffer.String())
            }

            spelling := "\\" + "u00" + fmt.Sprintf("%02x", continuation)
            if false == bytes.Contains(buffer.Bytes(), []byte(spelling)) {
                t.Fatalf("%s: expected the spelling %s in %q", format, spelling, buffer.String())
            }
        }

        decoded := map[string]any{}
        if decodeErr := json.Unmarshal(buffer.Bytes(), &decoded); nil != decodeErr {
            t.Fatalf("%s: expected a decodable document, got %v for %q", format, decodeErr, buffer.String())
        }

        data, isMap := decoded["data"].(map[string]any)
        if false == isMap || "a\xc2\x9bb" != data["name"] || "value" != data["k\xc2\x9dey"] {
            t.Fatalf("%s: expected the data decoded to the values given, got %#v", format, decoded["data"])
        }

        warnings, isList := decoded["warnings"].([]any)
        if false == isList || 1 != len(warnings) {
            t.Fatalf("%s: expected one warning, got %#v", format, decoded["warnings"])
        }

        warning, isWarning := warnings[0].(map[string]any)
        if false == isWarning || "wa\xc2\x85rn" != warning["message"] {
            t.Fatalf("%s: expected the warning decoded to the message given, got %#v", format, warnings[0])
        }
    }
}
