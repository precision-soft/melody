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
