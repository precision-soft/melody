package testhelper

import (
	"testing"

	"github.com/precision-soft/melody/v2/exception"
)

func AssertPanics(t *testing.T, callback func()) {
	if nil == t {
		exception.Panic(
			exception.NewError("testing t may not be nil", nil, nil),
		)
	}
	if nil == callback {
		exception.Panic(
			exception.NewError("callback may not be nil", nil, nil),
		)
	}

	t.Helper()

	defer func() {
		recoveredValue := recover()
		if nil == recoveredValue {
			t.Fatalf("expected panic")
		}
	}()

	callback()
}
