package output

import (
    "bytes"
    "strings"
    "testing"
    "unicode/utf8"
)

/** @info Hard-wrapping sliced cells on byte boundaries (splitLine[:width]) split multibyte UTF-8 runes in half, emitting invalid UTF-8 for any cell wider than the column. */
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

/** @info Column widths and cell padding were measured with len() (bytes); multibyte rows therefore misaligned the rendered border relative to the ASCII header/separator. */
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
