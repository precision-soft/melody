package output

func SelectPrinter(option Option) Printer {
    normalized := NormalizeOption(option)

    if FormatJson == normalized.Format {
        return &JsonPrinter{}
    }

    return NewDefaultTablePrinter()
}
