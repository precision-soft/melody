package internal

import (
    "strings"
    "testing"
)

type recoveredValuePlainPanic struct{}

func (instance recoveredValuePlainPanic) Error() string {
    panic("rendering exploded")
}

/* recoveredValueNestedPanic panics with a value whose own Error panics, the one input fmt does not contain */
type recoveredValueNestedPanic struct{}

func (instance recoveredValueNestedPanic) Error() string {
    panic(recoveredValuePlainPanic{})
}

func TestDescribeRecoveredValue_RendersAnOrdinaryValue(t *testing.T) {
    if "boom" != DescribeRecoveredValue("boom") {
        t.Fatalf("expected the value's own rendering, got %q", DescribeRecoveredValue("boom"))
    }
}

func TestDescribeRecoveredValue_NamesByTypeAValueWhoseRenderingPanicsTwice(t *testing.T) {
    text := DescribeRecoveredValue(recoveredValueNestedPanic{})

    if false == strings.Contains(text, "internal.recoveredValueNestedPanic") || false == strings.Contains(text, "rendering panicked") {
        t.Fatalf("expected the value to be named by its type, got %q", text)
    }
}
