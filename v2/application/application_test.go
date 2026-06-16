package application

import (
    "testing"

    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestAssertPanics_UsesRecover(t *testing.T) {
    testhelper.AssertPanics(t, func() {
        exception.Panic(exception.NewError("test", nil, nil))
    })
}
