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

/* @info a typed-nil pointer target passes the plain nil comparison, matches its case and dereferences nil on the assignment line — it must be refused with the same error the untyped nil gets, not answered with a panic on a method that promises an error */
func TestPlainTextSerializer_Deserialize_TypedNilTarget(t *testing.T) {
    serializer := NewPlainTextSerializer()

    var stringTarget *string
    err := serializer.Deserialize([]byte("x"), stringTarget)
    if nil == err {
        t.Fatalf("expected error for a typed-nil string target")
    }

    var bytesTarget *[]byte
    err = serializer.Deserialize([]byte("x"), bytesTarget)
    if nil == err {
        t.Fatalf("expected error for a typed-nil bytes target")
    }
}

/* @info the serialized payload is a snapshot the caller owns: a byte-slice value must be copied, not aliased, so a caller that reuses its buffer after Serialize cannot rewrite the bytes it believes it captured */
func TestPlainTextSerializer_Serialize_CopiesTheBytePayload(t *testing.T) {
    serializer := NewPlainTextSerializer()

    input := []byte("abc")

    data, err := serializer.Serialize(input)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    input[0] = 'x'

    if "abc" != string(data) {
        t.Fatalf("expected the serialized payload to own its bytes, got %q", string(data))
    }
}

/* @info the deserialized value owns its bytes: the payload must be copied into a byte-slice target, as the string case already does, so the caller reusing its read buffer cannot mutate the value it deserialized */
func TestPlainTextSerializer_Deserialize_CopiesThePayloadIntoTheTarget(t *testing.T) {
    serializer := NewPlainTextSerializer()

    payload := []byte("abc")

    var target []byte
    err := serializer.Deserialize(payload, &target)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    payload[0] = 'x'

    if "abc" != string(target) {
        t.Fatalf("expected the deserialized value to own its bytes, got %q", string(target))
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
