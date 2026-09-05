package output

import (
    "bytes"
    "encoding/json"
    "io"

    "github.com/precision-soft/melody/v2/internal"
)

type JsonPrinter struct {
}

/* the document is written on ONE line, terminated by the encoder's own newline, so a stream of them is a stream of records a line reader can hand to a parser whole — which is what a long-running command under --format=json is for, and what its own documentation promised while the indentation made every document a block of twenty. FormatJsonPretty is the same document with the indentation back, for the person reading it by hand; `| jq` does the same for a pipeline that already has it. */
func (instance *JsonPrinter) Print(
    writer io.Writer,
    envelope Envelope,
    option Option,
) error {
    document := &bytes.Buffer{}
    encoder := json.NewEncoder(document)

    if FormatJsonPretty == option.Format {
        encoder.SetIndent("", "  ")
    }

    encodeErr := encoder.Encode(envelope)
    if nil != encodeErr {
        return encodeErr
    }

    /* the encoder escapes the C0 block and the two Unicode line separators and leaves the C1 block raw, so a document carrying U+009B repainted the terminal it was printed to; the document is rewritten whole before it reaches the writer — the escape decodes to the same rune, so a consumer reads the value the command gave — and written once, so a write failure is still the writer's and still reported */
    _, writeErr := writer.Write(internal.EscapeJsonC1Block(document.Bytes()))
    if nil != writeErr {
        return writeErr
    }

    return nil
}

var _ Printer = (*JsonPrinter)(nil)
