package output

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"
    "unicode/utf8"
)

/* @info Hard-wrapping sliced cells on byte boundaries (splitLine[:width]) split multibyte UTF-8 runes in half, emitting invalid UTF-8 for any cell wider than the column. */
func TestTablePrinter_WrapsMultibyteRunesWithoutSplitting(t *testing.T) {
    printer := NewDefaultTablePrinter()

    /* "ăăăăă" is 5 runes but 10 bytes; at width 3 a byte slice cuts a codepoint in half */
    lines := printer.wrapCellValue("ăăăăă", 3)

    if 0 == len(lines) {
        t.Fatal("expected wrapped lines")
    }

    rebuilt := strings.Join(lines, "")
    if "ăăăăă" != rebuilt {
        t.Fatalf("wrapped lines must reconstruct the original value, got %q", rebuilt)
    }

    for index, line := range lines {
        if false == utf8.ValidString(line) {
            t.Fatalf("line %d is not valid UTF-8: %q (% x)", index, line, line)
        }
        if utf8.RuneCountInString(line) > 3 {
            t.Fatalf("line %d exceeds the column width in runes: %q", index, line)
        }
    }
}

/* @info Column widths and cell padding were measured with len() (bytes); multibyte rows therefore misaligned the rendered border relative to the ASCII header/separator. */
func TestTablePrinter_PadsMultibyteCellsByRune(t *testing.T) {
    printer := NewDefaultTablePrinter()

    envelope := Envelope{
        Table: &TableData{
            Blocks: []TableBlock{
                {
                    Columns: []string{"key", "value"},
                    Rows: [][]string{
                        {"k", "ăăă"},
                    },
                },
            },
        },
    }

    buffer := &bytes.Buffer{}
    if err := printer.Print(buffer, envelope, Option{Quiet: true}); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == utf8.ValidString(buffer.String()) {
        t.Fatalf("rendered table is not valid UTF-8: % x", buffer.String())
    }

    widthByRune := -1
    for _, line := range strings.Split(buffer.String(), "\n") {
        if false == strings.HasPrefix(line, "|") {
            continue
        }

        runeWidth := utf8.RuneCountInString(line)
        if -1 == widthByRune {
            widthByRune = runeWidth
            continue
        }
        if widthByRune != runeWidth {
            t.Fatalf("table rows are misaligned by rune width: %q has %d runes, expected %d", line, runeWidth, widthByRune)
        }
    }
}

/* @info the error is rendered whole, and regardless of quiet: this printer is the only presentation the default format has, and an envelope failure that rendered nowhere left the red one-line echo as the entire report — the code, the details and the cause existed only here */
func TestTablePrinter_RendersTheEnvelopeErrorUnderQuiet(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.SetError(
        "cmd.failed",
        "the command failed",
        map[string]any{
            "subject": "order",
        },
        NewErrorCause("the backend refused", map[string]any{"status": 503}),
    )

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Quiet = true

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()
    for _, expected := range []string{"ERROR: the command failed", "code: cmd.failed", "subject: order", "cause: the backend refused", "status: 503"} {
        if false == strings.Contains(written, expected) {
            t.Fatalf("expected %q in the rendered error, got %q", expected, written)
        }
    }
}

/* @info the warnings are printed under quiet as well: quiet suppresses the decorative headers, and a warning is the one thing a command said beside its result — swallowed by a default, it never reached anyone rendering the default table */
func TestTablePrinter_RendersTheWarningsUnderQuiet(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.AddWarning("cmd.checksum", "checksum mismatch", map[string]any{"file": "a.txt"})

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Quiet = true

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()
    if false == strings.Contains(written, "checksum mismatch") {
        t.Fatalf("expected the warning message under quiet, got %q", written)
    }
    if true == strings.Contains(written, "a.txt") {
        t.Fatalf("expected the warning details to stay behind the verbose flag, got %q", written)
    }
    if true == strings.Contains(written, "COMMAND:") {
        t.Fatalf("expected quiet to keep suppressing the headers, got %q", written)
    }
}

/* @info the constructor floors an unusable width at the default: zero is what a caller building an Option in code hands over without meaning "no room at all", and a negative one is what a misread flag produces — passed through, every column would shrink to its minimum and every cell would wrap to one rune per line */
func TestNewTablePrinter_FloorsAnUnusableWidthAtTheDefault(t *testing.T) {
    for _, requestedWidth := range []int{0, -1, -120} {
        printer := NewTablePrinter(requestedWidth)

        if defaultTableMaxWidth != printer.tableMaxWidth {
            t.Fatalf("expected width %d to be floored at %d, got %d", requestedWidth, defaultTableMaxWidth, printer.tableMaxWidth)
        }
    }

    printer := NewTablePrinter(40)
    if 40 != printer.tableMaxWidth {
        t.Fatalf("expected a usable width to be kept, got %d", printer.tableMaxWidth)
    }
}

/* @info the warning details are rendered under verbose, in key order: an unordered map walk printed the same warning differently on every run, which no diff of two reports can be read across */
func TestTablePrinter_RendersTheWarningDetailsSortedUnderVerbose(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.AddWarning(
        "cmd.checksum",
        "checksum mismatch",
        map[string]any{
            "zone": "eu",
            "file": "a.txt",
            "size": 12,
        },
    )

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Verbose = true

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()

    fileIndex := strings.Index(written, "file: a.txt")
    sizeIndex := strings.Index(written, "size: 12")
    zoneIndex := strings.Index(written, "zone: eu")

    if -1 == fileIndex || -1 == sizeIndex || -1 == zoneIndex {
        t.Fatalf("expected every warning detail under verbose, got %q", written)
    }
    if false == (fileIndex < sizeIndex && sizeIndex < zoneIndex) {
        t.Fatalf("expected the warning details in key order, got %q", written)
    }
}

/* @info a detail filed under no name is skipped: rendered, it prints as ": value" — a line an operator cannot attribute to anything */
func TestTablePrinter_SkipsAnUnnamedErrorDetail(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.SetError(
        "cmd.failed",
        "the command failed",
        map[string]any{
            "":        "filed under no name",
            "subject": "order",
        },
        nil,
    )

    buffer := &bytes.Buffer{}

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, DefaultOption())
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()
    if true == strings.Contains(written, "filed under no name") {
        t.Fatalf("expected the unnamed detail to be skipped, got %q", written)
    }
    if false == strings.Contains(written, "subject: order") {
        t.Fatalf("expected the named detail to be rendered, got %q", written)
    }
}

/* @info a row shorter than the declared columns is measured without reading past its end: the builder refuses such a row, but the printer also serves a TableData assembled by hand, and the sizing pass runs before anything renders */
func TestTablePrinter_MeasuresARowShorterThanTheColumns(t *testing.T) {
    printer := NewDefaultTablePrinter()

    block := TableBlock{
        Columns: []string{"name", "description"},
        Rows: [][]string{
            {"only-one-cell"},
        },
    }

    widths := printer.calculateColumnWidthsWithMaxWidth(block, defaultTableMaxWidth)

    if 2 != len(widths) {
        t.Fatalf("expected one width per declared column, got %d", len(widths))
    }
    if 13 != widths[0] {
        t.Fatalf("expected the first column to be sized by its only cell, got %d", widths[0])
    }
    if 11 != widths[1] {
        t.Fatalf("expected the second column to keep its header width, got %d", widths[1])
    }
}

/* @info a table wider than its budget is shrunk from the widest column down, never below the header it has to keep readable, and the surplus is wrapped instead of dropped */
func TestTablePrinter_ShrinksTheWidestColumnAndWrapsTheSurplus(t *testing.T) {
    const tableMaxWidth = 30

    printer := NewTablePrinter(tableMaxWidth)

    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.Table = &TableData{
        Blocks: []TableBlock{
            {
                Columns: []string{"name", "description"},
                Rows: [][]string{
                    {"a", "a description far wider than the budget allows"},
                },
            },
        },
    }

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Quiet = true

    printErr := printer.Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()

    renderedLineCount := 0
    for _, line := range strings.Split(written, "\n") {
        if false == strings.HasPrefix(line, "|") {
            continue
        }

        renderedLineCount = renderedLineCount + 1

        if utf8.RuneCountInString(line) > tableMaxWidth {
            t.Fatalf("expected every line within %d runes, got %d in %q", tableMaxWidth, utf8.RuneCountInString(line), line)
        }
    }

    /* the header, the separator and a row that no longer fits on one line */
    if 4 > renderedLineCount {
        t.Fatalf("expected the oversized row to wrap onto more than one line, got %d rendered lines in %q", renderedLineCount, written)
    }

    /* the widths themselves are the proof: the surplus is taken from the widest column, one rune at a time, and the narrow one stays at the minimum it may not go below */
    widths := printer.calculateColumnWidthsWithMaxWidth(envelope.Table.Blocks[0], tableMaxWidth)

    if 4 != widths[0] {
        t.Fatalf("expected the narrow column to stay at its header width, got %d", widths[0])
    }
    if 19 != widths[1] {
        t.Fatalf("expected the widest column to be shrunk to the remaining budget, got %d", widths[1])
    }
}

/* @info when every column already sits at its minimum there is nothing left to take, so the loop breaks instead of spinning: without the break it decrements nothing forever, because the width it would have to reduce is the one it refuses to */
func TestTablePrinter_StopsShrinkingWhenEveryColumnIsAtItsMinimum(t *testing.T) {
    printer := NewTablePrinter(1)

    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.Table = &TableData{
        Blocks: []TableBlock{
            {
                Columns: []string{"id", "name"},
                Rows: [][]string{
                    {"1", "first"},
                },
            },
        },
    }

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Quiet = true

    done := make(chan error, 1)
    go func() {
        done <- printer.Print(buffer, envelope, option)
    }()

    select {
    case printErr := <-done:
        if nil != printErr {
            t.Fatalf("expected no error, got %v", printErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("expected the shrink loop to stop when no column can give anything up")
    }

    /* the minimum wins over the budget: four runes per column is the floor, so a budget of one cannot be honoured and the row wraps rather than the loop spinning */
    widths := printer.calculateColumnWidthsWithMaxWidth(envelope.Table.Blocks[0], 1)

    if 2 != len(widths) || defaultTableMinColumnWidth != widths[0] || defaultTableMinColumnWidth != widths[1] {
        t.Fatalf("expected every column at the minimum width, got %v", widths)
    }

    if false == strings.Contains(buffer.String(), "firs") {
        t.Fatalf("expected the row to be rendered at the minimum widths, got %q", buffer.String())
    }
}

/* @info the two floors of the shrink pass, reached only from code: a block with no columns has nothing to shrink, and a width below one is not a budget — measured against them the loop would compute a negative budget and try to take width from columns that do not exist */
func TestTablePrinter_ShrinkPassFloors(t *testing.T) {
    printer := NewDefaultTablePrinter()

    emptyWidths := []int{}
    printer.shrinkWidthsToFitMaxWidth(TableBlock{}, emptyWidths, defaultTableMaxWidth)
    if 0 != len(emptyWidths) {
        t.Fatalf("expected a block with no columns to be left alone, got %v", emptyWidths)
    }

    widths := []int{40}
    block := TableBlock{Columns: []string{"name"}}

    printer.shrinkWidthsToFitMaxWidth(block, widths, 0)
    if 40 != widths[0] {
        t.Fatalf("expected a width below one to leave the columns untouched, got %v", widths)
    }

    printer.shrinkWidthsToFitMaxWidth(block, widths, -10)
    if 40 != widths[0] {
        t.Fatalf("expected a negative width to leave the columns untouched, got %v", widths)
    }
}

/* @info at a width of one there is no room to wrap into — a single rune per line would render a column of letters — so the value is answered whole and the row overflows visibly instead */
func TestTablePrinter_AnswersTheCellWholeWhenThereIsNoRoomToWrap(t *testing.T) {
    printer := NewDefaultTablePrinter()

    for _, width := range []int{1, 0, -1} {
        lines := printer.wrapCellValue("value", width)

        if 1 != len(lines) || "value" != lines[0] {
            t.Fatalf("expected the value answered whole at width %d, got %#v", width, lines)
        }
    }
}

/* @info an empty line inside a multi-line cell is kept as an empty line: dropped, the blank line separating two paragraphs of a description disappears and the two run together */
func TestTablePrinter_KeepsTheBlankLinesInsideAMultiLineCell(t *testing.T) {
    printer := NewDefaultTablePrinter()

    lines := printer.wrapCellValue("first\n\nthird", 10)

    if 3 != len(lines) {
        t.Fatalf("expected three lines, got %#v", lines)
    }
    if "first" != lines[0] || "" != lines[1] || "third" != lines[2] {
        t.Fatalf("expected the blank line to be kept in place, got %#v", lines)
    }
}

/* @info the carriage-return spellings are normalized before splitting, so a value produced on windows wraps the same way; and whatever the input, the answer is never an empty slice — printRowWrapped indexes into it per line */
func TestTablePrinter_NormalizesTheLineEndingsAndAlwaysAnswersALine(t *testing.T) {
    printer := NewDefaultTablePrinter()

    for _, value := range []string{"first\r\nsecond", "first\rsecond"} {
        lines := printer.wrapCellValue(value, 10)

        if 2 != len(lines) || "first" != lines[0] || "second" != lines[1] {
            t.Fatalf("expected %q to split into two lines, got %#v", value, lines)
        }
    }

    for _, value := range []string{"", "\n", "\n\n", "x"} {
        if 0 == len(printer.wrapCellValue(value, 10)) {
            t.Fatalf("expected at least one line for %q", value)
        }
    }
}

type failingTableWriter struct {
    failAfter int
    writes    int
}

func (instance *failingTableWriter) Write(payload []byte) (int, error) {
    instance.writes = instance.writes + 1
    if instance.writes > instance.failAfter {
        return 0, errors.New("no space left on device")
    }

    return len(payload), nil
}

/* @info the first write failure is remembered and returned: a report truncated by a full disk used to end with a success banner and exit zero */
func TestTablePrinter_ReturnsTheFirstWriteFailure(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))

    builder := NewTableBuilder()
    builder.AddBlock("BLOCK", []string{"name"}).AddRow("value")
    envelope.Table = builder.Build()

    printErr := NewDefaultTablePrinter().Print(&failingTableWriter{failAfter: 1}, envelope, DefaultOption())
    if nil == printErr {
        t.Fatalf("expected the write failure to be returned")
    }
    if "no space left on device" != printErr.Error() {
        t.Fatalf("expected the first write failure, got %v", printErr)
    }
}
