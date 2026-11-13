package output

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* SelectPrinter answers the printer the option asks for, refusing a TableMaxWidth its rule does not admit whatever the format. */
func SelectPrinter(option Option) (Printer, error) {
    normalized := normalizeOptionChoices(option)

    tableMaxWidth, resolveErr := bound.Resolve(normalized.TableMaxWidth, OptionTableMaxWidthRule)
    if nil != resolveErr {
        return nil, resolveErr
    }

    if true == IsJsonFormat(normalized.Format) {
        return &JsonPrinter{}, nil
    }

    if true == tableMaxWidth.Unlimited {
        return NewUnboundedTablePrinter(), nil
    }

    return NewTablePrinter(tableMaxWidth.Value), nil
}
