package audit

import (
    "encoding/json"
    "reflect"
    "strings"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

const redactedValue = "<redacted>"

var encryptedColumnType = reflect.TypeOf((*encrypt.EncryptedColumn)(nil)).Elem()
var baseModelType = reflect.TypeOf(bun.BaseModel{})

func isEncryptedColumnType(fieldType reflect.Type) bool {
    return fieldType.Implements(encryptedColumnType)
}

type Change struct {
    Field string `json:"field"`
    Old   any    `json:"old,omitempty"`
    New   any    `json:"new,omitempty"`

    oldPresent bool
    newPresent bool
}

type changeEnvelope struct {
    Field string `json:"field"`
    Old   *any   `json:"old,omitempty"`
    New   *any   `json:"new,omitempty"`
}

/* MarshalJSON emits each side by PRESENCE rather than by emptiness: under plain omitempty a genuine transition to a zero value — active true→false, a counter to 0, a string emptied — marshalled byte-identical to the one-sided shape of an insert or a delete, and a consumer of the trail could not tell "changed to false" from "no new value recorded". The walk records which sides exist, so a present zero renders (as false, 0, "" or null) and an absent side stays out. A Change built by hand keeps the old reading: with no presence recorded, a side is emitted when it is non-nil. */
func (instance Change) MarshalJSON() ([]byte, error) {
    envelope := changeEnvelope{Field: instance.Field}

    if true == instance.oldPresent || nil != instance.Old {
        oldValue := instance.Old
        envelope.Old = &oldValue
    }

    if true == instance.newPresent || nil != instance.New {
        newValue := instance.New
        envelope.New = &newValue
    }

    return json.Marshal(envelope)
}

func ChangeSet(before any, after any) []Change {
    return changeSetWithIgnore(before, after, nil)
}

func changeSetWithIgnore(before any, after any, ignore map[string]struct{}) []Change {
    var changes []Change

    seen := map[embedVisitKey]struct{}{}

    collectChanges(&changes, structValue(before, seen, false), structValue(after, seen, true), ignore, false, seen)

    if 0 == len(changes) {
        return []Change{}
    }

    return changes
}

type embedVisitKey struct {
    pointer uintptr
    after   bool
}

func collectChanges(changes *[]Change, beforeValue reflect.Value, afterValue reflect.Value, ignore map[string]struct{}, forceRedact bool, seen map[embedVisitKey]struct{}) {
    var structType reflect.Type
    if true == beforeValue.IsValid() {
        structType = beforeValue.Type()
    } else if true == afterValue.IsValid() {
        structType = afterValue.Type()
    } else {
        return
    }

    oldUsable := beforeValue.IsValid() && beforeValue.Type() == structType
    newUsable := afterValue.IsValid() && afterValue.Type() == structType

    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)
        if false == field.IsExported() && false == field.Anonymous {
            continue
        }

        if true == field.Anonymous {
            if false == isAuditableEmbed(field) {
                continue
            }

            var embeddedBefore reflect.Value
            var embeddedAfter reflect.Value
            beforeCycle := false
            afterCycle := false
            if true == oldUsable {
                embeddedBefore, beforeCycle = structValueOf(beforeValue.Field(index), seen, false)
            }
            if true == newUsable {
                embeddedAfter, afterCycle = structValueOf(afterValue.Field(index), seen, true)
            }

            if true == beforeCycle || true == afterCycle {
                continue
            }

            embedRedact := forceRedact || "redact" == field.Tag.Get("audit")
            collectChanges(changes, embeddedBefore, embeddedAfter, ignore, embedRedact, seen)
            continue
        }

        name, skip := auditFieldName(field)
        if true == skip {
            continue
        }

        if _, ignored := ignore[name]; true == ignored {
            continue
        }

        redact := forceRedact || isRedactedField(field)

        var oldValue any
        var newValue any
        if true == oldUsable {
            oldValue = beforeValue.Field(index).Interface()
        }
        if true == newUsable {
            newValue = afterValue.Field(index).Interface()
        }

        if false == redact {
            redact = valueContainsRedactTag(oldValue) || valueContainsRedactTag(newValue)
        }

        if true == oldUsable && true == newUsable {
            if true == valuesEqual(oldValue, newValue) {
                continue
            }
            if true == redact {
                *changes = append(*changes, Change{Field: name, Old: redactedValue, New: redactedValue, oldPresent: true, newPresent: true})
                continue
            }
            *changes = append(*changes, Change{Field: name, Old: oldValue, New: newValue, oldPresent: true, newPresent: true})
            continue
        }

        if true == newUsable {
            if true == redact {
                *changes = append(*changes, Change{Field: name, New: redactedValue, newPresent: true})
                continue
            }
            *changes = append(*changes, Change{Field: name, New: newValue, newPresent: true})
            continue
        }

        if true == redact {
            *changes = append(*changes, Change{Field: name, Old: redactedValue, oldPresent: true})
            continue
        }

        *changes = append(*changes, Change{Field: name, Old: oldValue, oldPresent: true})
    }
}

func isAuditableEmbed(field reflect.StructField) bool {
    if "-" == field.Tag.Get("bun") {
        return false
    }

    embedded := dereferencePointerType(field.Type)

    if reflect.Struct != embedded.Kind() {
        return false
    }

    return baseModelType != embedded && false == isEncryptedColumnType(embedded) && reflect.TypeOf(time.Time{}) != embedded
}

func structValueOf(value reflect.Value, seen map[embedVisitKey]struct{}, after bool) (reflect.Value, bool) {
    for reflect.Ptr == value.Kind() {
        if true == value.IsNil() {
            return reflect.Value{}, false
        }

        key := embedVisitKey{pointer: value.Pointer(), after: after}
        if _, visited := seen[key]; true == visited {
            return reflect.Value{}, true
        }
        seen[key] = struct{}{}

        value = value.Elem()
    }

    if reflect.Struct != value.Kind() {
        return reflect.Value{}, false
    }

    return value, false
}

func valuesEqual(left any, right any) bool {
    leftTime, leftIsTime := asTime(left)
    rightTime, rightIsTime := asTime(right)
    if true == leftIsTime && true == rightIsTime {
        return leftTime.Equal(rightTime)
    }

    return reflect.DeepEqual(left, right)
}

func asTime(value any) (time.Time, bool) {
    switch typed := value.(type) {
    case time.Time:
        return typed, true
    case *time.Time:
        if nil == typed {
            return time.Time{}, false
        }
        return *typed, true
    default:
        return time.Time{}, false
    }
}

func structValue(value any, seen map[embedVisitKey]struct{}, after bool) reflect.Value {
    if nil == value {
        return reflect.Value{}
    }

    resolved, _ := structValueOf(reflect.ValueOf(value), seen, after)

    return resolved
}

func dereferencePointerType(pointerType reflect.Type) reflect.Type {
    visited := map[reflect.Type]struct{}{}

    for reflect.Ptr == pointerType.Kind() {
        if _, seen := visited[pointerType]; true == seen {
            return pointerType
        }
        visited[pointerType] = struct{}{}

        pointerType = pointerType.Elem()
    }

    return pointerType
}

func isRedactedField(field reflect.StructField) bool {
    if "redact" == field.Tag.Get("audit") {
        return true
    }

    fieldType := dereferencePointerType(field.Type)

    if true == isEncryptedColumnType(fieldType) {
        return true
    }

    return typeContainsRedactTag(field.Type, map[reflect.Type]struct{}{})
}

func valueContainsRedactTag(value any) bool {
    if nil == value {
        return false
    }

    return valueContainsRedactTagReflect(reflect.ValueOf(value), map[redactVisitKey]struct{}{})
}

type redactVisitKey struct {
    pointer uintptr
    length  uintptr
}

func valueContainsRedactTagReflect(value reflect.Value, seen map[redactVisitKey]struct{}) bool {
    for reflect.Ptr == value.Kind() || reflect.Interface == value.Kind() {
        if true == value.IsNil() {
            return false
        }
        if reflect.Ptr == value.Kind() {
            key := redactVisitKey{pointer: value.Pointer()}
            if _, visited := seen[key]; true == visited {
                return false
            }
            seen[key] = struct{}{}
        }
        value = value.Elem()
    }

    if false == value.IsValid() {
        return false
    }

    valueType := value.Type()
    if true == isEncryptedColumnType(valueType) {
        return true
    }

    switch value.Kind() {
    case reflect.Slice:

        pointer := value.Pointer()
        if 0 != pointer {

            key := redactVisitKey{pointer: pointer, length: uintptr(value.Len()) + 1}
            if _, visited := seen[key]; true == visited {
                return false
            }
            seen[key] = struct{}{}
        }
        for index := 0; index < value.Len(); index++ {
            if true == valueContainsRedactTagReflect(value.Index(index), seen) {
                return true
            }
        }
        return false

    case reflect.Array:
        for index := 0; index < value.Len(); index++ {
            if true == valueContainsRedactTagReflect(value.Index(index), seen) {
                return true
            }
        }
        return false

    case reflect.Map:
        mapKey := redactVisitKey{pointer: value.Pointer()}
        if _, visited := seen[mapKey]; true == visited {
            return false
        }
        seen[mapKey] = struct{}{}
        for _, key := range value.MapKeys() {
            if true == valueContainsRedactTagReflect(key, seen) {
                return true
            }
            if true == valueContainsRedactTagReflect(value.MapIndex(key), seen) {
                return true
            }
        }
        return false

    case reflect.Struct:
        for index := 0; index < valueType.NumField(); index++ {
            subField := valueType.Field(index)
            if false == subField.IsExported() {
                continue
            }

            if "redact" == subField.Tag.Get("audit") {
                return true
            }

            if true == isEncryptedColumnType(dereferencePointerType(subField.Type)) {
                return true
            }

            if true == valueContainsRedactTagReflect(value.Field(index), seen) {
                return true
            }
        }
        return false

    default:
        return false
    }
}

func typeContainsRedactTag(fieldType reflect.Type, seen map[reflect.Type]struct{}) bool {
    for reflect.Ptr == fieldType.Kind() || reflect.Slice == fieldType.Kind() || reflect.Array == fieldType.Kind() {
        if _, visited := seen[fieldType]; true == visited {
            return false
        }
        seen[fieldType] = struct{}{}

        fieldType = fieldType.Elem()
    }

    if _, visited := seen[fieldType]; true == visited {
        return false
    }
    seen[fieldType] = struct{}{}

    if reflect.Map == fieldType.Kind() {
        if true == typeContainsRedactTag(fieldType.Key(), seen) {
            return true
        }
        return typeContainsRedactTag(fieldType.Elem(), seen)
    }

    if reflect.Struct != fieldType.Kind() {
        return false
    }

    for index := 0; index < fieldType.NumField(); index++ {
        subField := fieldType.Field(index)
        if false == subField.IsExported() {
            continue
        }

        if "redact" == subField.Tag.Get("audit") {
            return true
        }

        if true == isEncryptedColumnType(dereferencePointerType(subField.Type)) {
            return true
        }

        if true == typeContainsRedactTag(subField.Type, seen) {
            return true
        }
    }

    return false
}

func auditFieldName(field reflect.StructField) (string, bool) {
    if true == field.Anonymous {
        return "", true
    }

    tag := field.Tag.Get("bun")
    if "-" == tag {
        return "", true
    }

    if "" == tag {
        return field.Name, false
    }

    first := tag
    if comma := strings.IndexByte(tag, ','); -1 != comma {
        first = tag[:comma]
    }

    if true == strings.HasPrefix(first, "rel:") || true == strings.HasPrefix(first, "m2m:") {
        return "", true
    }

    if "" == first {
        return field.Name, false
    }

    return first, false
}
