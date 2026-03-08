package bag

import "testing"

func TestBagStringStrict(t *testing.T) {
    parameterBag := NewParameterBag()

    _, exists, err := StringStrict(parameterBag, "name")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected exists to be false")
    }

    parameterBag.Set("name", nil)
    value, exists, err := StringStrict(parameterBag, "name")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists to be true")
    }
    if "" != value {
        t.Fatalf("expected empty string for nil value, got %q", value)
    }

    parameterBag.Set("name", "")
    value, exists, err = StringStrict(parameterBag, "name")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists to be true")
    }
    if "" != value {
        t.Fatalf("expected empty string, got %q", value)
    }

    parameterBag.Set("name", "value")
    value, exists, err = StringStrict(parameterBag, "name")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists to be true")
    }
    if "value" != value {
        t.Fatalf("expected value %q, got %q", "value", value)
    }

    parameterBag.Set("name", 123)
    _, exists, err = StringStrict(parameterBag, "name")
    if false == exists {
        t.Fatalf("expected exists to be true even when conversion fails")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
    if "parameter is not a 'string'" != err.Error() {
        t.Fatalf("unexpected message: %q", err.Error())
    }
}
