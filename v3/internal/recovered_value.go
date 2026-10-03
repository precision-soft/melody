package internal

import (
    "fmt"
)

/* DescribeRecoveredValue renders a recovered panic value with %v inside the defer reporting it, under a recover of its own: fmt contains a panicking Error or String, but not one that panics with a value whose own rendering panics, which would raise a second panic out of the recovery. Such a value is named by its type, which calls none of its methods. */
func DescribeRecoveredValue(value any) (text string) {
    defer func() {
        if nil == recover() {
            return
        }

        text = fmt.Sprintf("a value of type %T whose rendering panicked", value)
    }()

    return fmt.Sprintf("%v", value)
}
