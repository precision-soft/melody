package debug

import (
    "strings"
    "testing"
    "unicode/utf8"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

/** @info truncateTableCellValue must not split a multibyte UTF-8 rune when it cuts an over-long cell */
func TestTruncateTableCellValue_KeepsRunesIntactOnMultibyteOverflow(t *testing.T) {
    value := strings.Repeat("ș", 115) /* 230 bytes, over the 220 byte cap; the 217 byte cut lands mid rune */

    result := truncateTableCellValue(value)

    if false == utf8.ValidString(result) {
        t.Fatalf("truncated cell is not valid UTF-8: %q", result)
    }
    if false == strings.HasSuffix(result, "...") {
        t.Fatalf("truncated cell must end with an ellipsis, got %q", result)
    }
    if len(result) > 220 {
        t.Fatalf("truncated cell exceeds the byte cap: %d bytes", len(result))
    }
}

/** @info wrapFixedWidth must not split a multibyte UTF-8 rune at the wrap boundary */
func TestWrapFixedWidth_KeepsRunesIntactAtBoundary(t *testing.T) {
    /* the leading ASCII byte shifts every 2 byte rune onto an odd offset, so the even width boundary lands mid rune on the unpatched code */
    value := "a" + strings.Repeat("ș", 60) /* 121 bytes */

    lines := wrapFixedWidth(value, 80)

    for index, line := range lines {
        if false == utf8.ValidString(line) {
            t.Fatalf("wrapped line %d is not valid UTF-8: %q", index, line)
        }
    }

    if strings.Join(lines, "") != value {
        t.Fatalf("wrapped lines do not reconstruct the original value")
    }
}

/** @info resolveErrorContextJson must strip stack keys even when marshalling fails and it falls back to fmt */
func TestResolveErrorContextJson_RedactsStackOnMarshalFailure(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "stack":      "SECRET_STACK_TRACE",
        "panicStack": "SECRET_PANIC_STACK",
        "channel":    make(chan int), /* not JSON marshalable, so it forces the fmt fallback */
    }

    resolveErr := exception.NewError("boom", contextValue, nil)

    result := resolveErrorContextJson(resolveErr, 0)

    if true == strings.Contains(result, "SECRET_STACK_TRACE") {
        t.Fatalf("stack value leaked into the fallback output: %q", result)
    }
    if true == strings.Contains(result, "SECRET_PANIC_STACK") {
        t.Fatalf("panicStack value leaked into the fallback output: %q", result)
    }
}
