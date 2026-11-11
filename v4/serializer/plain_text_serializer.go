package serializer

import (
    "fmt"
    "math"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/plaintext"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
)

/* PlainTextSerializerConfig bounds the lines a structured value is written as; each field left out takes its default. The error body a text/plain client reads is written under the defaults. */
type PlainTextSerializerConfig struct {
    /* MaxDepth is how deep the walk descends through maps, structs and lists before it writes an ellipsis: 8 by default, 1 to 1,024, unlimited refused, since a value may hold itself and the walk recurses once per level. */
    MaxDepth bound.Count

    /* MaxLines is how many lines a body holds before it ends with ...(truncated): 1,024 by default, at least 1, unlimited admitted. */
    MaxLines bound.Count

    /* MaxBytes is how many bytes of keys and values a body spends before it ends with ...(truncated), the indentation, two spaces a level, not counted: 262,144 by default, at least 1, unlimited admitted. */
    MaxBytes bound.Count

    /* MaxMembers is how many values a body walks before it ends with ...(truncated), since a value reaching itself through many members would otherwise be walked a number of times exponential in the depth: 16,384 by default, at least 1, unlimited admitted. */
    MaxMembers bound.Count
}

var plainTextMaxDepthRule = bound.Rule[int]{
    Name:    "PlainTextSerializerConfig.MaxDepth",
    Default: plaintext.DefaultMaxDepth,
    Minimum: 1,
    Maximum: maxPlainTextDepth,
}

/* maxPlainTextDepth bounds the walk's recursion, which no recover contains once it runs out of memory */
const maxPlainTextDepth = 1024

var plainTextMaxLinesRule = bound.Rule[int]{
    Name:              "PlainTextSerializerConfig.MaxLines",
    Default:           plaintext.DefaultMaxLines,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var plainTextMaxBytesRule = bound.Rule[int]{
    Name:              "PlainTextSerializerConfig.MaxBytes",
    Default:           plaintext.DefaultMaxBytes,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var plainTextMaxMembersRule = bound.Rule[int]{
    Name:              "PlainTextSerializerConfig.MaxMembers",
    Default:           plaintext.DefaultMaxMembers,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

/* NewPlainTextSerializer builds the text/plain serializer; a bound outside its rule is refused with a panic naming the field. */
func NewPlainTextSerializer(config PlainTextSerializerConfig) *PlainTextSerializer {
    return &PlainTextSerializer{
        options: plaintext.Options{
            MaxDepth:   resolvePlainTextBound(config.MaxDepth, plainTextMaxDepthRule),
            MaxLines:   resolvePlainTextBound(config.MaxLines, plainTextMaxLinesRule),
            MaxBytes:   resolvePlainTextBound(config.MaxBytes, plainTextMaxBytesRule),
            MaxMembers: resolvePlainTextBound(config.MaxMembers, plainTextMaxMembersRule),
        },
    }
}

func resolvePlainTextBound(limit bound.Count, rule bound.Rule[int]) int {
    resolved, resolveErr := bound.Resolve(limit, rule)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    if true == resolved.Unlimited {
        return math.MaxInt
    }

    return resolved.Value
}

/* PlainTextSerializer writes a string and a byte slice as they are, a scalar, an error, an encoding.TextMarshaler or a fmt.Stringer as its text, whole, and a map, a struct or a list as lines: one key: value line per entry in key order, a struct by the fields and names encoding/json writes, a json.Marshaler by the json it writes, a nested map or struct indented beneath its key, one - line per item of a list, a body of lines ending with a newline. A method that panics and a marshaler that fails are answered as an error. */
type PlainTextSerializer struct {
    options plaintext.Options
}

func (instance *PlainTextSerializer) Serialize(value any) (payload []byte, serializeErr error) {
    switch typedValue := value.(type) {
    case string:
        return []byte(typedValue), nil
    case []byte:
        /* the payload is a copy, so a caller reusing its buffer after Serialize does not change it */
        copied := make([]byte, len(typedValue))
        copy(copied, typedValue)

        return copied, nil
    }

    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        payload = nil
        serializeErr = exception.NewError(
            "the plain text serializer could not write the value",
            exceptioncontract.Context{
                "valueType": fmt.Sprintf("%T", value),
                "panic":     internal.DescribeRecoveredValue(recovered),
            },
            nil,
        )
    }()

    return []byte(plaintext.Render(value, instance.options)), nil
}

func (instance *PlainTextSerializer) Deserialize(payload []byte, target any) error {
    /* a typed-nil pointer target is refused as the untyped nil is, as the json serializer refuses it */
    if true == internal.IsNilInterface(target) {
        return exception.NewError("deserialize target is nil", nil, nil)
    }

    switch typedTarget := target.(type) {
    case *string:
        *typedTarget = string(payload)
        return nil
    case *[]byte:
        /* the deserialized value owns its bytes */
        copied := make([]byte, len(payload))
        copy(copied, payload)
        *typedTarget = copied
        return nil
    default:
        return exception.NewError(
            "unsupported plain text deserialize target type",
            exceptioncontract.Context{
                "targetType": fmt.Sprintf("%T", target),
            },
            nil,
        )
    }
}

func (instance *PlainTextSerializer) ContentType() string {
    return MimeTextPlain + "; charset=utf-8"
}

var _ serializercontract.Serializer = (*PlainTextSerializer)(nil)
