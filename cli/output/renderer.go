package output

import "io"

func Render(
	writer io.Writer,
	envelope Envelope,
	option Option,
) error {
	printer := SelectPrinter(option)

	printErr := printer.Print(writer, envelope, option)
	if nil != printErr {
		return printErr
	}

	return nil
}
