package audit

import (
    "reflect"
    "strings"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

const redactedValue = "<redacted>"

var encryptedStringType = reflect.TypeOf(encrypt.EncryptedString(""))
var encryptedDeterministicStringType = reflect.TypeOf(encrypt.EncryptedDeterministicString(""))
var baseModelType = reflect.TypeOf(bun.BaseModel{})

type Change struct {
    Field string `json:"field"`
    Old   any    `json:"old,omitempty"`
    New   any    `json:"new,omitempty"`
}

func ChangeSet(before any, after any) []Change {
    return changeSetWithIgnore(before, after, nil)
}

func changeSetWithIgnore(before any, after any, ignore map[string]struct{}) []Change {
    var changes []Change
    collectChanges(&changes, structValue(before), structValue(after), ignore)

    return changes
}

func collectChanges(changes *[]Change, beforeValue reflect.Value, afterValue reflect.Value, ignore map[string]struct{}) {
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
        if false == field.IsExported() {
            continue
        }

        if true == field.Anonymous {
            if false == isAuditableEmbed(field) {
                continue
            }

            var embeddedBefore reflect.Value
            var embeddedAfter reflect.Value
            if true == oldUsable {
                embeddedBefore = structValueOf(beforeValue.Field(index))
            }
            if true == newUsable {
                embeddedAfter = structValueOf(afterValue.Field(index))
            }

            collectChanges(changes, embeddedBefore, embeddedAfter, ignore)
            continue
        }

        name, skip := auditFieldName(field)
        if true == skip {
            continue
        }

        if _, ignored := ignore[name]; true == ignored {
            continue
        }

        redact := isRedactedField(field)

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
                *changes = append(*changes, Change{Field: name, Old: redactedValue, New: redactedValue})
                continue
            }
            *changes = append(*changes, Change{Field: name, Old: oldValue, New: newValue})
            continue
        }

        if true == newUsable {
            if true == redact {
                *changes = append(*changes, Change{Field: name, New: redactedValue})
                continue
            }
            *changes = append(*changes, Change{Field: name, New: newValue})
            continue
        }

        if true == redact {
            *changes = append(*changes, Change{Field: name, Old: redactedValue})
            continue
        }

        *changes = append(*changes, Change{Field: name, Old: oldValue})
    }
}

func isAuditableEmbed(field reflect.StructField) bool {
    if "-" == field.Tag.Get("bun") {
        return false
    }

    embedded := field.Type
    for reflect.Ptr == embedded.Kind() {
        embedded = embedded.Elem()
    }

    if reflect.Struct != embedded.Kind() {
        return false
    }

    return baseModelType != embedded && encryptedStringType != embedded && encryptedDeterministicStringType != embedded && reflect.TypeOf(time.Time{}) != embedded
}

func structValueOf(value reflect.Value) reflect.Value {
    for reflect.Ptr == value.Kind() {
        if true == value.IsNil() {
            return reflect.Value{}
        }
        value = value.Elem()
    }

    if reflect.Struct != value.Kind() {
        return reflect.Value{}
    }

    return value
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

func structValue(value any) reflect.Value {
    if nil == value {
        return reflect.Value{}
    }

    reflected := reflect.ValueOf(value)
    for reflect.Ptr == reflected.Kind() {
        if true == reflected.IsNil() {
            return reflect.Value{}
        }
        reflected = reflected.Elem()
    }

    if reflect.Struct != reflected.Kind() {
        return reflect.Value{}
    }

    return reflected
}

func isRedactedField(field reflect.StructField) bool {
    if "redact" == field.Tag.Get("audit") {
        return true
    }

    fieldType := field.Type
    for reflect.Ptr == fieldType.Kind() {
        fieldType = fieldType.Elem()
    }

    if fieldType == encryptedStringType || fieldType == encryptedDeterministicStringType {
        return true
    }

    return typeContainsRedactTag(field.Type, map[reflect.Type]struct{}{})
}

func valueContainsRedactTag(value any) bool {
    if nil == value {
        return false
    }

    return typeContainsRedactTag(reflect.TypeOf(value), map[reflect.Type]struct{}{})
}

func typeContainsRedactTag(fieldType reflect.Type, seen map[reflect.Type]struct{}) bool {
    for reflect.Ptr == fieldType.Kind() || reflect.Slice == fieldType.Kind() || reflect.Array == fieldType.Kind() {
        fieldType = fieldType.Elem()
    }

    if reflect.Map == fieldType.Kind() {
        return typeContainsRedactTag(fieldType.Elem(), seen)
    }

    if reflect.Struct != fieldType.Kind() {
        return false
    }

    if _, visited := seen[fieldType]; true == visited {
        return false
    }
    seen[fieldType] = struct{}{}

    for index := 0; index < fieldType.NumField(); index++ {
        subField := fieldType.Field(index)
        if false == subField.IsExported() {
            continue
        }

        if "redact" == subField.Tag.Get("audit") {
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

    if true == strings.HasPrefix(first, "rel:") {
        return "", true
    }

    if "" == first {
        return field.Name, false
    }

    return first, false
}
