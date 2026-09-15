package cache

import (
    "bytes"
    "crypto/sha256"
    "encoding/gob"
    "encoding/hex"
    "reflect"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
)

var cachedValueList = []any{
    &entity.Product{},
    &entity.Category{},
    &entity.Currency{},
    &entity.User{},
}

func NewGobSerializer() melodycachecontract.Serializer {
    for _, value := range cachedValueList {
        gob.Register(value)
        gob.Register(reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(value)), 0, 0).Interface())
    }

    return &gobSerializer{}
}

/* LayoutToken hashes cached type names and exported field layouts recursively. Prefixes using it isolate incompatible gob layouts. Keys from older layouts remain orphaned until explicitly removed; the current-layout reset does not clear them. */
func LayoutToken() string {
    return layoutTokenOf(cachedValueList)
}

func layoutTokenOf(valueList []any) string {
    descriptionList := make([]string, 0, len(valueList))
    for _, value := range valueList {
        descriptionList = append(descriptionList, layoutDescriptionOf(reflect.TypeOf(value), map[reflect.Type]bool{}))
    }

    sort.Strings(descriptionList)

    digest := sha256.Sum256([]byte(strings.Join(descriptionList, "\n")))

    return hex.EncodeToString(digest[:layoutTokenByteLength])
}

const layoutTokenByteLength = 6

func layoutDescriptionOf(valueType reflect.Type, visited map[reflect.Type]bool) string {
    for reflect.Pointer == valueType.Kind() || reflect.Slice == valueType.Kind() || reflect.Array == valueType.Kind() {
        valueType = valueType.Elem()
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

    encodeErr := encoder.Encode(&interfaceValue)
    if nil != encodeErr {
        return nil, encodeErr
    }

    return buffer.Bytes(), nil
}

func (instance *gobSerializer) Deserialize(payload []byte) (any, error) {
    buffer := bytes.NewBuffer(payload)
    decoder := gob.NewDecoder(buffer)

    var value any
    decodeErr := decoder.Decode(&value)
    if nil != decodeErr {
        return nil, decodeErr
    }

    return value, nil
}

var _ melodycachecontract.Serializer = (*gobSerializer)(nil)
