package cli

import (
    "io"
    "os"
    "strings"
    "testing"
)

/* capturedOutput runs the printer against a pipe standing in for the process's own output, because the three helpers report through fmt.Println rather than answering a value. The descriptor is restored whatever the printer did. */
func capturedOutput(t *testing.T, print func()) string {
    t.Helper()

    reader, writer, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("open a pipe: %v", pipeErr)
    }

    original := os.Stdout
    os.Stdout = writer

    done := make(chan string, 1)
    go func() {
        content, _ := io.ReadAll(reader)
        done <- string(content)
    }()

    print()

    os.Stdout = original
    if closeErr := writer.Close(); nil != closeErr {
        t.Fatalf("close the pipe: %v", closeErr)
    }

    return <-done
}

func tableLines(t *testing.T, headers []string, rows [][]string) []string {
    t.Helper()

    return strings.Split(strings.TrimRight(capturedOutput(t, func() { printTable(headers, rows) }), "\n"), "\n")
}

/* Every column is as wide as its widest cell, header included, so a value longer than the heading widens the column rather than pushing the rest of the row out of line. */
func TestPrintTableSizesEveryColumnToItsWidestCell(t *testing.T) {
    lines := tableLines(
        t,
        []string{"ID", "NAME"},
        [][]string{
            {"prod-1", "Keyboard"},
            {"prod-10", "A very long product name"},
        },
    )

    if 4 != len(lines) {
        t.Fatalf("expected a header, a separator and two rows, got %d lines: %q", len(lines), lines)
    }

    for index, line := range lines {
        if len(lines[0]) != len(line) {
            t.Fatalf("line %d is %d wide, the header is %d: %q", index, len(line), len(lines[0]), line)
        }
    }

    if false == strings.HasPrefix(lines[0], "ID     ") {
        t.Fatalf("the first column was not widened to its widest cell: %q", lines[0])
    }
}

/* The separator is drawn to the same widths as the rows, with the joint under the column divider rather than beside it. */
func TestPrintSeparatorMatchesTheColumnWidths(t *testing.T) {
    lines := tableLines(t, []string{"ID", "NAME"}, [][]string{{"prod-1", "Keyboard"}})

    separator := lines[1]

    if len(lines[0]) != len(separator) {
        t.Fatalf("the separator is %d wide, the header is %d", len(separator), len(lines[0]))
    }

    if false == strings.Contains(separator, "--+--") {
        t.Fatalf("the separator carries no joint: %q", separator)
    }

    if strings.Index(lines[0], "|") != strings.Index(separator, "+") {
        t.Fatalf("the joint is not under the divider: %q / %q", lines[0], separator)
    }
}

func TestPrintRowSeparatesTheColumnsWithTheDivider(t *testing.T) {
    output := capturedOutput(t, func() { printRow([]string{"a", "bb"}, []int{1, 2}) })

    if "a  |  bb\n" != output {
        t.Fatalf("unexpected row: %q", output)
    }
}

/* A cell exactly as wide as its column gets no padding at all, which is the boundary the padding arithmetic is written against: one character further and the subtraction would go negative, and strings.Repeat panics on a negative count rather than answering an empty string. */
func TestPrintRowPadsNothingWhenTheCellFillsTheColumn(t *testing.T) {
    output := capturedOutput(t, func() { printRow([]string{"abc"}, []int{3}) })

    if "abc\n" != output {
        t.Fatalf("unexpected row: %q", output)
    }
}

/* The width is the BYTE length of the cell, not the number of characters a terminal draws. A catalogue name outside ascii therefore over-pads its column, and the table the two commands share loses its alignment for exactly the rows that carry one. The framework's own cli/output table printer is the door that measures this properly; the example does not use it yet, and this assertion records what it does today rather than what it should do. */
func TestPrintTableMeasuresCellsInBytes(t *testing.T) {
    lines := tableLines(t, []string{"NAME"}, [][]string{{"Café"}})

    if 5 != len(lines[1]) {
        t.Fatalf("expected a separator of five bytes for a four character cell, got %d: %q", len(lines[1]), lines[1])
    }
}
