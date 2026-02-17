package output

import (
	"encoding/json"
	"io"
)

type JsonPrinter struct {
}

func (instance *JsonPrinter) Print(
	writer io.Writer,
	envelope Envelope,
	option Option,
) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	encodeErr := encoder.Encode(envelope)
	if nil != encodeErr {
		return encodeErr
	}

	return nil
}

var _ Printer = (*JsonPrinter)(nil)
