package output

import (
    "fmt"
    "io"
    "sort"
    "strings"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v2/internal"
)

/* escapeLine keeps a single-line channel single-line and inert on a terminal: the summary lines, titles, warnings and error texts carry values an http client may have written earlier, and an embedded carriage return or escape sequence would repaint or forge what the report appears to say. The escaping is visible rather than silent — the operator reads \x1b where the terminal would have obeyed it. */
func escapeLine(value string) string {
    return internal.EscapeControlCharacters(value)
}

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

/* errorTrackingWriter remembers the first write failure and swallows the rest: the table is printed through dozens of small writes, and threading every result through the row and cell helpers would bury the layout code. A report truncated by a full disk used to end with a success banner and exit zero; the remembered failure is what lets Print refuse instead. */
type errorTrackingWriter struct {
    writer   io.Writer
    firstErr error
}

func (instance *errorTrackingWriter) Write(payload []byte) (int, error) {
    if nil != instance.firstErr {
        return len(payload), nil
    }

    _, writeErr := instance.writer.Write(payload)
    if nil != writeErr {
        instance.firstErr = writeErr
    }

    return len(payload), nil
}

func (instance *TablePrinter) Print(
    writer io.Writer,
    envelope Envelope,
    option Option,
) error {
    tracking := &errorTrackingWriter{writer: writer}
    writer = tracking

    if false == option.Quiet {
        _, _ = fmt.Fprintf(writer, "COMMAND: %s\n", escapeLine(envelope.Meta.Command))
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

    if nil != envelope.Table {
        for _, line := range envelope.Table.SummaryLines {
            _, _ = fmt.Fprintln(writer, escapeLine(line))
        }
        if 0 != len(envelope.Table.SummaryLines) {
            _, _ = fmt.Fprintln(writer)
        }

        for _, block := range envelope.Table.Blocks {
            if "" != block.Title {
                _, _ = fmt.Fprintln(writer, escapeLine(block.Title))
            }

            instance.printTableBlock(writer, block)

            _, _ = fmt.Fprintln(writer)
        }
    }

    /* the warnings are printed under quiet as well: quiet suppresses the decorative headers, and a warning is the one thing a command said beside its result — swallowed by a default, it never reached anyone rendering the default table. Only the warning details stay behind the verbose flag. */
    if 0 != len(envelope.Warnings) {
        _, _ = fmt.Fprintln(writer, "WARNINGS:")
        for _, warning := range envelope.Warnings {
            _, _ = fmt.Fprintf(writer, "- %s\n", escapeLine(warning.Message))

            if true == option.Verbose && nil != warning.Details && 0 != len(warning.Details) {
                keys := make([]string, 0, len(warning.Details))
                for key := range warning.Details {
                    keys = append(keys, key)
                }
                sort.Strings(keys)

                for _, key := range keys {
                    _, _ = fmt.Fprintf(writer, "  %s: %s\n", escapeLine(key), escapeLine(fmt.Sprintf("%v", warning.Details[key])))
                }
            }
        }
    }

    /* the error is rendered whole, and regardless of quiet: this printer is the only presentation the default format has, and an envelope failure that renders nowhere leaves the red one-line echo as the entire report — the code, the details and the cause existed only here */
    if nil != envelope.Error {
        _, _ = fmt.Fprintf(writer, "ERROR: %s\n", escapeLine(envelope.Error.Message))

        if "" != envelope.Error.Code {
            _, _ = fmt.Fprintf(writer, "  code: %s\n", escapeLine(envelope.Error.Code))
        }

        instance.printSortedDetails(writer, envelope.Error.Details, "  ")

        if nil != envelope.Error.Cause {
            _, _ = fmt.Fprintf(writer, "  cause: %s\n", escapeLine(envelope.Error.Cause.Message))
            instance.printSortedDetails(writer, envelope.Error.Cause.Details, "    ")
        }
    }

    return tracking.firstErr
}

func (instance *TablePrinter) printSortedDetails(writer io.Writer, details map[string]any, indent string) {
    if 0 == len(details) {
        return
    }

    keys := make([]string, 0, len(details))
    for key := range details {
        if "" == key {
            continue
        }

        keys = append(keys, key)
    }
    sort.Strings(keys)

    for _, key := range keys {
        _, _ = fmt.Fprintf(writer, "%s%s: %s\n", indent, escapeLine(key), escapeLine(fmt.Sprintf("%v", details[key])))
    }
}

func (instance *TablePrinter) printTableBlock(writer io.Writer, block TableBlock) {
    block = sanitizeTableBlock(block)

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

/* sanitizeTableBlock escapes the control characters of every column and cell before the widths are measured, so the escaped spelling is the one the width arithmetic counts and the one the wrap slices — escaping at print time instead would render wider than it measured. A newline stays a real line break, because a cell renders multi-line on purpose, and a separator row keeps its token so the separator detection still recognizes it. */
func sanitizeTableBlock(block TableBlock) TableBlock {
    sanitizedColumns := make([]string, len(block.Columns))
    for index, column := range block.Columns {
        sanitizedColumns[index] = internal.EscapeControlCharactersKeepingNewlines(column)
    }

    sanitizedRows := make([][]string, len(block.Rows))
    for rowIndex, row := range block.Rows {
        if 1 == len(row) && TableRowSeparatorToken == row[0] {
            sanitizedRows[rowIndex] = row

            continue
        }

        sanitizedRow := make([]string, len(row))
        for cellIndex, cell := range row {
            sanitizedRow[cellIndex] = internal.EscapeControlCharactersKeepingNewlines(cell)
        }
        sanitizedRows[rowIndex] = sanitizedRow
    }

    return TableBlock{
        Title:   block.Title,
        Columns: sanitizedColumns,
        Rows:    sanitizedRows,
    }
}

func (instance *TablePrinter) calculateColumnWidthsWithMaxWidth(block TableBlock, maxWidth int) []int {
    columnCount := len(block.Columns)
    widths := make([]int, columnCount)

    for index, column := range block.Columns {
        widths[index] = utf8.RuneCountInString(column)
    }

    for _, row := range block.Rows {
        if true == instance.isSeparatorRow(row) {
            continue
        }

        for index := 0; index < columnCount; index++ {
            if index >= len(row) {
                continue
            }
            cellWidth := utf8.RuneCountInString(row[index])
            if widths[index] < cellWidth {
                widths[index] = cellWidth
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
        columnWidth := utf8.RuneCountInString(block.Columns[index])
        if minWidth < columnWidth {
            minWidth = columnWidth
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

            valueWidth := utf8.RuneCountInString(value)
            if valueWidth < width {
                value = value + strings.Repeat(" ", width-valueWidth)
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

        splitRunes := []rune(splitLine)
        for 0 < len(splitRunes) {
            if len(splitRunes) <= width {
                lines = append(lines, string(splitRunes))
                break
            }

            lines = append(lines, string(splitRunes[:width]))
            splitRunes = splitRunes[width:]
        }
    }

    if 0 == len(lines) {
        return []string{""}
    }

    return lines
}

var _ Printer = (*TablePrinter)(nil)
