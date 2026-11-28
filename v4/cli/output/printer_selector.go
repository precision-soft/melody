package output

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* SelectPrinter answers the printer the option asks for, refusing a TableMaxWidth its rule does not admit whatever the format. */
func SelectPrinter(options Options) (Printer, error) {
    normalized := normalizeOptionsChoices(options)

    tableMaxWidth, resolveErr := bound.Resolve(normalized.TableMaxWidth, OptionsTableMaxWidthRule())
    if nil != resolveErr {
        return nil, resolveErr
    }

    if true == IsJsonFormat(normalized.Format) {
        return &JsonPrinter{}, nil
    }

    return NewTablePrinter(TablePrinterConfig{MaxWidth: tableMaxWidth.Limit()}), nil
}
