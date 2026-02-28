package serializer

import "testing"

func TestPlainTextSerializer_Serialize_String(t *testing.T) {
    serializer := NewPlainTextSerializer()

    data, err := serializer.Serialize("x")
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != string(data) {
        t.Fatalf("unexpected payload")
    }
}

func TestPlainTextSerializer_Serialize_Bytes(t *testing.T) {
    serializer := NewPlainTextSerializer()

    data, err := serializer.Serialize([]byte("x"))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != string(data) {
        t.Fatalf("unexpected payload")
    }
}

func TestPlainTextSerializer_Deserialize_StringTarget(t *testing.T) {
    serializer := NewPlainTextSerializer()

    var target string
    err := serializer.Deserialize([]byte("x"), &target)
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != target {
        t.Fatalf("unexpected target")
    }
}

func TestPlainTextSerializer_Deserialize_BytesTarget(t *testing.T) {
    serializer := NewPlainTextSerializer()

    var target []byte
    err := serializer.Deserialize([]byte("x"), &target)
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != string(target) {
        t.Fatalf("unexpected target")
    }
}

func TestPlainTextSerializer_Deserialize_NilTarget(t *testing.T) {
    serializer := NewPlainTextSerializer()

    err := serializer.Deserialize([]byte("x"), nil)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestPlainTextSerializer_Deserialize_UnsupportedTarget(t *testing.T) {
    serializer := NewPlainTextSerializer()

    var target int
    err := serializer.Deserialize([]byte("x"), &target)
    if nil == err {
        t.Fatalf("expected error")
    }
}
