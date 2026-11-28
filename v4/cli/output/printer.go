package output

import "io"

type Printer interface {
    Print(
        writer io.Writer,
        envelope Envelope,
        options Options,
    ) error
}
