package output

import (
    "bytes"
    "encoding/json"
    "io"

    "github.com/precision-soft/melody/internal"
)

type JsonPrinter struct {
}

/* the document is written on one line terminated by the encoder's newline, so a stream of them is a stream of records a line reader hands to a parser whole; FormatJsonPretty indents it for a person */
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

    /* the encoder leaves the C1 block raw, so U+009B could repaint a terminal: the document is rewritten whole with those escaped, each escape decoding to the same rune, and written once, so a write failure is still reported */
    escaped := internal.EscapeJsonC1Block(document.Bytes())

    written, writeErr := writer.Write(escaped)
    if nil != writeErr {
        return writeErr
    }

    /* a sink that accepts fewer bytes than the document without an error has truncated it, so the shortfall is a printing failure, as in the table printer */
    if written < len(escaped) {
        return io.ErrShortWrite
    }

    return nil
}

var _ Printer = (*JsonPrinter)(nil)
