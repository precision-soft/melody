package output

import (
    "bytes"
    "encoding/json"
    "io"

    "github.com/precision-soft/melody/v3/internal"
)

type JsonPrinter struct {
}

/* Print emits compact JSON as one newline-terminated record. FormatJsonPretty uses indented output instead. */
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

    _, writeErr := writer.Write(internal.EscapeJsonC1Block(document.Bytes()))
    if nil != writeErr {
        return writeErr
    }

    return nil
}

var _ Printer = (*JsonPrinter)(nil)
