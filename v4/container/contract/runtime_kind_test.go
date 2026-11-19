package contract

import (
    "testing"
)

func TestKind_StringNamesEveryKind(t *testing.T) {
    for kind, expected := range map[Kind]string{
        KindUnspecified: "unspecified",
        KindBoot:        "boot",
        KindRequest:     "request",
        KindRun:         "run",
        KindDetached:    "detached",
        Kind(200):       "unspecified",
    } {
        if expected != kind.String() {
            t.Fatalf("expected %q for kind %d, got %q", expected, kind, kind.String())
        }
    }
}
