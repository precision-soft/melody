package contract

import (
    "context"
    "testing"
)

type nilContext struct {
    context.Context
}

func TestRefuseNilContext_RefusesANilAndATypedNilContext(t *testing.T) {
    var typedNil *nilContext

    for _, refused := range []context.Context{nil, typedNil} {
        refusalErr := RefuseNilContext(refused)
        if nil == refusalErr || "session context is nil" != refusalErr.Error() {
            t.Fatalf("expected the context %#v to be refused, got %v", refused, refusalErr)
        }
    }

    if admittedErr := RefuseNilContext(context.Background()); nil != admittedErr {
        t.Fatalf("expected a live context to be admitted, got %v", admittedErr)
    }
}
