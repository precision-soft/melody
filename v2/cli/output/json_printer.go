package output

import (
    "encoding/json"
    "io"
)

type JsonPrinter struct {
}

/* the document is written on ONE line, terminated by the encoder's own newline, so a stream of them is a stream of records a line reader can hand to a parser whole — which is what a long-running command under --format=json is for, and what its own documentation promised while the indentation made every document a block of twenty. FormatJsonPretty is the same document with the indentation back, for the person reading it by hand; `| jq` does the same for a pipeline that already has it. */
func (instance *JsonPrinter) Print(
    writer io.Writer,
    envelope Envelope,
    option Option,
) error {
    encoder := json.NewEncoder(writer)

    if FormatJsonPretty == option.Format {
        encoder.SetIndent("", "  ")
    }

    encodeErr := encoder.Encode(envelope)
    if nil != encodeErr {
        return encodeErr
    }

    return nil
}

var _ Printer = (*JsonPrinter)(nil)
