package output

import "io"

type Printer interface {
	Print(
		writer io.Writer,
		envelope Envelope,
		option Option,
	) error
}
