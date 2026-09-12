package output

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/internal"
)

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

/* the builder refuses a row shorter than the declared columns, so this state is reachable only through a TableData assembled by hand — which the printer also serves, and the sizing pass runs before anything renders */
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

/* the two floors of the shrink pass are reached only from code, never through the printer's own doors, so the state is built here directly. */
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

func TestTablePrinter_AnswersTheCellWholeWhenThereIsNoRoomToWrap(t *testing.T) {
    printer := NewDefaultTablePrinter()

    for _, width := range []int{1, 0, -1} {
        lines := printer.wrapCellValue("value", width)

        if 1 != len(lines) || "value" != lines[0] {
            t.Fatalf("expected the value answered whole at width %d, got %#v", width, lines)
        }
    }
}

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

/* the negative assertions are the proof: a raw escape byte or carriage return in the output would be obeyed by the terminal, and both render identically to the eye in a green test that only checks the positive spelling. */
func TestTablePrinter_EscapesControlCharactersInTheTextChannels(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.SetError(
        "cmd.failed",
        "open failed: file\rERROR: disk gone\x1b[2J",
        nil,
        nil,
    )
    envelope.AddWarning("warning.code", "careful\x1b]0;pwned\x07", nil)
    envelope.Table = &TableData{
        SummaryLines: []string{"listing\rforged"},
    }

    buffer := &bytes.Buffer{}

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, DefaultOption())
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()

    for _, forbidden := range []string{"\x1b", "\r", "\x07"} {
        if true == strings.Contains(written, forbidden) {
            t.Fatalf("expected no raw control character in the output, found %q in %q", forbidden, written)
        }
    }

    for _, expected := range []string{`file\rERROR: disk gone\x1b[2J`, `careful\x1b]0;pwned\x07`, `listing\rforged`} {
        if false == strings.Contains(written, expected) {
            t.Fatalf("expected the escaped spelling %q in %q", expected, written)
        }
    }
}

/* the escaping runs before the widths are measured, so the escaped spelling is what the alignment counts — escaped at print time instead, the cell renders wider than it measured and the row breaks out of its column. */
func TestTablePrinter_EscapedCellsStayAligned(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.Table = &TableData{
        Blocks: []TableBlock{
            {
                Columns: []string{"NAME", "NOTE"},
                Rows: [][]string{
                    {"widget\x1b[31m", "plain"},
                    {"other", "row"},
                },
            },
        },
    }

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Quiet = true

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    written := buffer.String()

    if true == strings.Contains(written, "\x1b") {
        t.Fatalf("expected no raw escape byte in the table, got %q", written)
    }
    if false == strings.Contains(written, `widget\x1b[31m`) {
        t.Fatalf("expected the escaped cell spelling, got %q", written)
    }

    rowWidth := -1
    for _, line := range strings.Split(written, "\n") {
        if false == strings.HasPrefix(line, "|") {
            continue
        }

        lineWidth := utf8.RuneCountInString(line)
        if -1 == rowWidth {
            rowWidth = lineWidth

            continue
        }

        if rowWidth != lineWidth {
            t.Fatalf("expected every table row to hold the same width, got %d and %d in %q", rowWidth, lineWidth, written)
        }
    }
}

/* the alignment oracle is display cells, not runes: with the rune measure a CJK cell measured 2 and rendered 4, so its own row's closing pipe sat two cells past every other row's */
func TestTablePrinter_CjkCellsStayAlignedInDisplayCells(t *testing.T) {
    envelope := NewEnvelope(NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{}))
    envelope.Table = &TableData{
        Blocks: []TableBlock{
            {
                Columns: []string{"NAME", "NOTE"},
                Rows: [][]string{
                    {"世界", "cjk"},
                    {"plain", "row"},
                },
            },
        },
    }

    buffer := &bytes.Buffer{}

    option := DefaultOption()
    option.Quiet = true

    printErr := NewDefaultTablePrinter().Print(buffer, envelope, option)
    if nil != printErr {
        t.Fatalf("expected no error, got %v", printErr)
    }

    rowWidth := -1
    for _, line := range strings.Split(buffer.String(), "\n") {
        if false == strings.HasPrefix(line, "|") {
            continue
        }

        lineWidth := internal.DisplayWidth(line)
        if -1 == rowWidth {
            rowWidth = lineWidth

            continue
        }

        if rowWidth != lineWidth {
            t.Fatalf("expected every table row to render the same display width, got %d and %d in %q", rowWidth, lineWidth, buffer.String())
        }
    }
    if -1 == rowWidth {
        t.Fatalf("expected table rows in the output, got %q", buffer.String())
    }
}

/* the wrap slices by the same display-cell measure the widths are computed in: sliced by rune count, a line of two ideograms and a line of four ascii letters both "fit" a width of four while rendering four and eight cells */
func TestTablePrinter_WrapsByDisplayCellsNotRunes(t *testing.T) {
    printer := NewDefaultTablePrinter()

    lines := printer.wrapCellValue("世界世", 4)

    if 2 != len(lines) {
        t.Fatalf("expected two lines, got %#v", lines)
    }
    if "世界" != lines[0] || "世" != lines[1] {
        t.Fatalf("expected the ideograms sliced two cells each, got %#v", lines)
    }
}

/* this printer keeps a newline as a real line break, so a cell's width is the width of its widest LINE. Measured whole, "12345\n67890" reads eleven cells wide — a column sized for text no line ever renders. */
func TestTablePrinter_MeasuresAMultiLineCellByItsWidestLine(t *testing.T) {
    printer := NewDefaultTablePrinter()

    envelope := Envelope{
        Table: &TableData{
            Blocks: []TableBlock{
                {
                    Columns: []string{"trace", "note"},
                    Rows: [][]string{
                        {"12345\n67890", "ok"},
                    },
                },
            },
        },
    }

    buffer := &bytes.Buffer{}
    if err := printer.Print(buffer, envelope, Option{Quiet: true}); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    separator := ""
    for _, line := range strings.Split(buffer.String(), "\n") {
        if true == strings.HasPrefix(line, "| -") {
            separator = line
        }
    }
    if "" == separator {
        t.Fatalf("expected a separator row in the output, got %q", buffer.String())
    }

    if "| ----- | ---- |" != separator {
        t.Fatalf("expected the multi-line cell measured by its widest line (5), got %q", separator)
    }
}

/* the phantom width a whole-string measure invents is counted into the maximum-width arithmetic, so the sibling columns are shrunk to pay for it */
func TestTablePrinter_DoesNotShrinkASiblingColumnForAMultiLineCellsPhantomWidth(t *testing.T) {
    printer := NewTablePrinter(30)

    tallCell := strings.TrimSuffix(strings.Repeat("abc\n", 10), "\n")

    envelope := Envelope{
        Table: &TableData{
            Blocks: []TableBlock{
                {
                    Columns: []string{"trace", "description"},
                    Rows: [][]string{
                        {tallCell, "abcdefghijklmno"},
                    },
                },
            },
        },
    }

    buffer := &bytes.Buffer{}
    if err := printer.Print(buffer, envelope, Option{Quiet: true}); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    separator := ""
    for _, line := range strings.Split(buffer.String(), "\n") {
        if true == strings.HasPrefix(line, "| -") {
            separator = line
        }
    }
    if "" == separator {
        t.Fatalf("expected a separator row in the output, got %q", buffer.String())
    }

    /* ten lines of three cells measure three, not thirty-nine: the table fits the cap whole and neither column is shrunk */
    if "| ----- | --------------- |" != separator {
        t.Fatalf("expected both columns at their measured widths, got %q", separator)
    }

    if false == strings.Contains(buffer.String(), "abcdefghijklmno") {
        t.Fatalf("expected the sibling cell to render unwrapped, got %q", buffer.String())
    }
}

/* the width helper is the one place the multi-line measure lives, and it answers the widest line for every shape the sanitized cell can take */
func TestCellDisplayWidth_AnswersTheWidestLine(t *testing.T) {
    if 5 != cellDisplayWidth("12345\n67890") {
        t.Fatalf("expected the widest line of two equal lines, got %d", cellDisplayWidth("12345\n67890"))
    }

    if 7 != cellDisplayWidth("ab\nabcdefg\nabc") {
        t.Fatalf("expected the widest of three unequal lines, got %d", cellDisplayWidth("ab\nabcdefg\nabc"))
    }

    if 4 != cellDisplayWidth("世界") {
        t.Fatalf("expected a single line measured in display cells, got %d", cellDisplayWidth("世界"))
    }

    if 0 != cellDisplayWidth("") {
        t.Fatalf("expected an empty cell to measure nothing, got %d", cellDisplayWidth(""))
    }

    if 0 != cellDisplayWidth("\n\n") {
        t.Fatalf("expected empty lines to measure nothing, got %d", cellDisplayWidth("\n\n"))
    }
}

/* a column header is sanitized keeping its newlines and wrapped by the same renderer, so it is measured by its widest line exactly as a cell is — on both the measure pass and the minimum the shrink pass floors it at */
func TestTablePrinter_MeasuresAMultiLineColumnHeaderByItsWidestLine(t *testing.T) {
    printer := NewDefaultTablePrinter()

    envelope := Envelope{
        Table: &TableData{
            Blocks: []TableBlock{
                {
                    Columns: []string{"abcde\nfg", "note"},
                    Rows: [][]string{
                        {"x", "ok"},
                    },
                },
            },
        },
    }

    buffer := &bytes.Buffer{}
    if err := printer.Print(buffer, envelope, Option{Quiet: true}); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    separator := ""
    for _, line := range strings.Split(buffer.String(), "\n") {
        if true == strings.HasPrefix(line, "| -") {
            separator = line
        }
    }
    if "" == separator {
        t.Fatalf("expected a separator row in the output, got %q", buffer.String())
    }

    /* the header's widest line is five cells; measured whole it reads eight */
    if "| ----- | ---- |" != separator {
        t.Fatalf("expected the multi-line header measured by its widest line (5), got %q", separator)
    }
}
