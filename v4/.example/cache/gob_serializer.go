package cache

import (
    "bytes"
    "crypto/sha256"
    "encoding/gob"
    "encoding/hex"
    "fmt"
    "reflect"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v4/.example/entity"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
)

/* cachedValueList is every value this application puts in the cache through gob, spelled once: the serializer registers each of them, and the layout token below is computed over exactly the same list, so a type cached but not registered — or registered but not tokened — cannot exist. */
var cachedValueList = []any{
    &entity.Product{},
    &entity.Category{},
    &entity.Currency{},
    &entity.UserProfile{},
}

func NewGobSerializer() melodycachecontract.Serializer {
    for _, value := range cachedValueList {
        gob.Register(value)
        gob.Register(reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(value)), 0, 0).Interface())
    }

    return &gobSerializer{}
}

/* LayoutToken is a short hash of the layout of every cached type (its name and, field by field, each exported field's name and type, into the structs it holds), and the cache keys carry it in their prefix. gob decodes by field name and stays silent about a field the payload lacks, so without it a build that added a field would read the zero from every older entry until its ttl lapsed; under a prefix that changes with the layout a build reads only what its own layout wrote, and older entries stand orphaned under their own token, which the reset of the current namespace does not reach, until they lapse with their ttl. The token is computed rather than maintained, because a version bumped by hand is one someone forgets. */
func LayoutToken() string {
    return layoutTokenOf(cachedValueList)
}

/* layoutTokenOf is the token over an explicit list, so the property — the token moves when a field does — is pinned on a type of the test's own rather than on the entities it exists to watch. */
func layoutTokenOf(valueList []any) string {
    descriptionList := make([]string, 0, len(valueList))
    for _, value := range valueList {
        descriptionList = append(descriptionList, layoutDescriptionOf(reflect.TypeOf(value), map[reflect.Type]bool{}))
    }

    sort.Strings(descriptionList)

    digest := sha256.Sum256([]byte(strings.Join(descriptionList, "\n")))

    return hex.EncodeToString(digest[:layoutTokenByteLength])
}

/* layoutTokenByteLength is the width of the token in the prefix: six bytes, twelve hex characters, is a collision space of 2^48 over a list of four layouts, which is the whole of what the token separates. */
const layoutTokenByteLength = 6

/* layoutDescriptionOf spells a type's layout as the decoder reads it: the type's own name, and for a struct each exported field's name with the description of its type, pointers and slices looked through, a struct already on the path named rather than re-entered. */
func layoutDescriptionOf(valueType reflect.Type, visited map[reflect.Type]bool) string {
    for reflect.Pointer == valueType.Kind() || reflect.Slice == valueType.Kind() || reflect.Array == valueType.Kind() {
        valueType = valueType.Elem()
    }

    /* a map is looked through to its key and its element the way a slice is to its element: a struct held as a map value is decoded by gob by field name exactly like one held directly, so a field added to it has to move the token too. An interface stays opaque — its dynamic type is not in the layout. */
    if reflect.Map == valueType.Kind() {
        return "map[" + layoutDescriptionOf(valueType.Key(), visited) + "]" + layoutDescriptionOf(valueType.Elem(), visited)
    }

    if reflect.Struct != valueType.Kind() {
        return valueType.String()
    }

    if true == visited[valueType] {
        return valueType.String()
    }

    visited[valueType] = true

    fieldList := make([]string, 0, valueType.NumField())
    for index := 0; index < valueType.NumField(); index++ {
        field := valueType.Field(index)
        if false == field.IsExported() {
            continue
        }

        fieldList = append(fieldList, field.Name+" "+layoutDescriptionOf(field.Type, visited))
    }

    return valueType.String() + "{" + strings.Join(fieldList, "; ") + "}"
}

type gobSerializer struct{}

func (instance *gobSerializer) Serialize(value any) ([]byte, error) {
    buffer := &bytes.Buffer{}
    encoder := gob.NewEncoder(buffer)

    var interfaceValue = value

    /* gob refuses a nil pointer inside an interface, and a typed read answers a remembered absence as one, so it travels as the nil interface Deserialize reads back as the zero value */
    if reflectedValue := reflect.ValueOf(value); reflect.Pointer == reflectedValue.Kind() && true == reflectedValue.IsNil() {
        interfaceValue = nil
    }

    encodeErr := encoder.Encode(&interfaceValue)
    if nil != encodeErr {
        return nil, encodeErr
    }

    return buffer.Bytes(), nil
}

func (instance *gobSerializer) Deserialize(payload []byte, target any) error {
    targetValue := reflect.ValueOf(target)
    if reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() {
        return fmt.Errorf("gob cache target %T is not a non-nil pointer", target)
    }

    buffer := bytes.NewBuffer(payload)
    decoder := gob.NewDecoder(buffer)

    var value any
    decodeErr := decoder.Decode(&value)
    if nil != decodeErr {
        return decodeErr
    }

    elementValue := targetValue.Elem()
    if nil == value {
        elementValue.SetZero()

        return nil
    }

    decodedValue := reflect.ValueOf(value)
    if false == decodedValue.Type().AssignableTo(elementValue.Type()) {
        return fmt.Errorf("gob cache payload holds %s, which a %s target cannot hold", decodedValue.Type(), elementValue.Type())
    }

    elementValue.Set(decodedValue)

    return nil
}

var _ melodycachecontract.Serializer = (*gobSerializer)(nil)
