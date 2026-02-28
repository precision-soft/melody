package output

import (
    "fmt"
    "io"
    "sort"
    "strings"
    "time"
)

const TableRowSeparatorToken = "__melody_table_separator__"

const defaultTableMinColumnWidth = 4

const defaultTableMaxWidth = 120

func NewTablePrinter(tableMaxWidth int) *TablePrinter {
    normalizedTableMaxWidth := tableMaxWidth
    if 1 > normalizedTableMaxWidth {
        normalizedTableMaxWidth = defaultTableMaxWidth
    }

    return &TablePrinter{
        tableMaxWidth: normalizedTableMaxWidth,
    }
}

func NewDefaultTablePrinter() *TablePrinter {
    return NewTablePrinter(defaultTableMaxWidth)
}

type TablePrinter struct {
    tableMaxWidth int
}

func (instance *TablePrinter) Print(
    writer io.Writer,
    envelope Envelope,
    option Option,
) error {
    if false == option.Quiet {
        _, _ = fmt.Fprintf(writer, "COMMAND: %s\n", envelope.Meta.Command)
        _, _ = fmt.Fprintf(
            writer,
            "STARTED AT: %s\n",
            envelope.Meta.StartedAt.Format(time.DateTime),
        )

        if "" != envelope.Meta.Version.Application {
            _, _ = fmt.Fprintf(writer, "APPLICATION: %s\n", envelope.Meta.Version.Application)
        }
        if "" != envelope.Meta.Version.Melody {
            _, _ = fmt.Fprintf(writer, "MELODY: %s\n", envelope.Meta.Version.Melody)
        }
        if "" != envelope.Meta.Version.Go {
            _, _ = fmt.Fprintf(writer, "GO: %s\n", envelope.Meta.Version.Go)
        }

        _, _ = fmt.Fprintln(writer)
    }

    if nil == envelope.Table {
        return nil
    }

    for _, line := range envelope.Table.SummaryLines {
        _, _ = fmt.Fprintln(writer, line)
    }
    if 0 != len(envelope.Table.SummaryLines) {
        _, _ = fmt.Fprintln(writer)
    }

    for _, block := range envelope.Table.Blocks {
        if "" != block.Title {
            _, _ = fmt.Fprintln(writer, block.Title)
        }

        instance.printTableBlock(writer, block)

        _, _ = fmt.Fprintln(writer)
    }

    if 0 != len(envelope.Warnings) && false == option.Quiet {
        _, _ = fmt.Fprintln(writer, "WARNINGS:")
        for _, warning := range envelope.Warnings {
            _, _ = fmt.Fprintf(writer, "- %s\n", warning.Message)

            if true == option.Verbose && nil != warning.Details && 0 != len(warning.Details) {
                keys := make([]string, 0, len(warning.Details))
                for key := range warning.Details {
                    keys = append(keys, key)
                }
                sort.Strings(keys)

                for _, key := range keys {
                    _, _ = fmt.Fprintf(writer, "  %s: %v\n", key, warning.Details[key])
                }
            }
        }
    }

    return nil
}

func (instance *TablePrinter) printTableBlock(writer io.Writer, block TableBlock) {
    columnWidths := instance.calculateColumnWidthsWithMaxWidth(block, instance.tableMaxWidth)

    instance.printRowWrapped(writer, block.Columns, columnWidths)
    instance.printSeparator(writer, columnWidths)

    previousWasSeparator := true

    for _, row := range block.Rows {
        if true == instance.isSeparatorRow(row) {
            if true == previousWasSeparator {
                continue
            }

            instance.printSeparator(writer, columnWidths)
            previousWasSeparator = true

            continue
        }

        instance.printRowWrapped(writer, row, columnWidths)
        previousWasSeparator = false
    }
}

func (instance *TablePrinter) calculateColumnWidthsWithMaxWidth(block TableBlock, maxWidth int) []int {
    columnCount := len(block.Columns)
    widths := make([]int, columnCount)

    for index, column := range block.Columns {
        widths[index] = len(column)
    }

    for _, row := range block.Rows {
        if true == instance.isSeparatorRow(row) {
            continue
        }

        for index := 0; index < columnCount; index++ {
            if index >= len(row) {
                continue
            }
            if widths[index] < len(row[index]) {
                widths[index] = len(row[index])
            }
        }
    }

    instance.shrinkWidthsToFitMaxWidth(block, widths, maxWidth)

    return widths
}

func (instance *TablePrinter) shrinkWidthsToFitMaxWidth(block TableBlock, widths []int, maxWidth int) {
    if 0 == len(widths) {
        return
    }
    if 1 > maxWidth {
        return
    }

    columnCount := len(widths)
    minWidths := make([]int, columnCount)

    for index := 0; index < columnCount; index++ {
        minWidth := defaultTableMinColumnWidth
        if minWidth < len(block.Columns[index]) {
            minWidth = len(block.Columns[index])
        }
        minWidths[index] = minWidth

        if widths[index] < minWidth {
            widths[index] = minWidth
        }
    }

    availableContentWidth := maxWidth - 4 - (3 * (columnCount - 1))
    if 1 > availableContentWidth {
        availableContentWidth = 1
    }

    currentContentWidth := 0
    for _, width := range widths {
        currentContentWidth = currentContentWidth + width
    }

    for currentContentWidth > availableContentWidth {
        widestIndex := -1
        widestWidth := 0

        for index := 0; index < columnCount; index++ {
            if widths[index] <= minWidths[index] {
                continue
            }

            if widths[index] > widestWidth {
                widestWidth = widths[index]
                widestIndex = index
            }
        }

        if -1 == widestIndex {
            break
        }

        widths[widestIndex] = widths[widestIndex] - 1
        currentContentWidth = currentContentWidth - 1
    }
}

func (instance *TablePrinter) isSeparatorRow(row []string) bool {
    if 1 != len(row) {
        return false
    }

    return TableRowSeparatorToken == row[0]
}

func (instance *TablePrinter) printSeparator(writer io.Writer, widths []int) {
    cells := make([]string, len(widths))
    for index, width := range widths {
        cells[index] = strings.Repeat("-", width)
    }

    _, _ = fmt.Fprintf(writer, "| %s |\n", strings.Join(cells, " | "))
}

func (instance *TablePrinter) printRowWrapped(writer io.Writer, cells []string, widths []int) {
    wrappedCells := make([][]string, len(widths))
    maxLines := 1

    for index, width := range widths {
        value := ""
        if index < len(cells) {
            value = cells[index]
        }

        lines := instance.wrapCellValue(value, width)
        wrappedCells[index] = lines
        if maxLines < len(lines) {
            maxLines = len(lines)
        }
    }

    for lineIndex := 0; lineIndex < maxLines; lineIndex++ {
        lineCells := make([]string, len(widths))
        for cellIndex, width := range widths {
            value := ""
            if lineIndex < len(wrappedCells[cellIndex]) {
                value = wrappedCells[cellIndex][lineIndex]
            }

            if len(value) < width {
                value = value + strings.Repeat(" ", width-len(value))
            }
            lineCells[cellIndex] = value
        }

        _, _ = fmt.Fprintf(writer, "| %s |\n", strings.Join(lineCells, " | "))
    }
}

func (instance *TablePrinter) wrapCellValue(value string, width int) []string {
    if "" == value {
        return []string{""}
    }
    if 1 >= width {
        return []string{value}
    }

    normalized := strings.ReplaceAll(value, "\r\n", "\n")
    normalized = strings.ReplaceAll(normalized, "\r", "\n")

    split := strings.Split(normalized, "\n")
    if 0 == len(split) {
        return []string{""}
    }

    lines := make([]string, 0, len(split))
    for _, splitLine := range split {
        if "" == splitLine {
            lines = append(lines, "")
            continue
        }

        for 0 < len(splitLine) {
            if len(splitLine) <= width {
                lines = append(lines, splitLine)
                break
            }

            lines = append(lines, splitLine[:width])
            splitLine = splitLine[width:]
        }
    }

    if 0 == len(lines) {
        return []string{""}
    }

    return lines
}

var _ Printer = (*TablePrinter)(nil)
