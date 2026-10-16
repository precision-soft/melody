package output

func SelectPrinter(option Option) Printer {
    normalized := NormalizeOption(option)

    if true == IsJsonFormat(normalized.Format) {
        return &JsonPrinter{}
    }

    if 0 < normalized.TableMaxWidth {
        return NewTablePrinter(normalized.TableMaxWidth)
    }

    return NewDefaultTablePrinter()
}
