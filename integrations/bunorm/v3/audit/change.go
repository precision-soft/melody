package audit

import (
    "reflect"
    "strings"
    "time"
)

type Change struct {
    Field string `json:"field"`
    Old   any    `json:"old,omitempty"`
    New   any    `json:"new,omitempty"`
}

func ChangeSet(before any, after any) []Change {
    beforeValue := structValue(before)
    afterValue := structValue(after)

    var structType reflect.Type
    if true == beforeValue.IsValid() {
        structType = beforeValue.Type()
    } else if true == afterValue.IsValid() {
        structType = afterValue.Type()
    } else {
        return nil
    }

    oldUsable := beforeValue.IsValid() && beforeValue.Type() == structType
    newUsable := afterValue.IsValid() && afterValue.Type() == structType

    var changes []Change

    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)
        if false == field.IsExported() {
            continue
        }

        name, skip := auditFieldName(field)
        if true == skip {
            continue
        }

        oldPresent := oldUsable
        newPresent := newUsable

        var oldValue any
        var newValue any
        if true == oldPresent {
            oldValue = beforeValue.Field(index).Interface()
        }
        if true == newPresent {
            newValue = afterValue.Field(index).Interface()
        }

        if true == oldPresent && true == newPresent {
            if true == valuesEqual(oldValue, newValue) {
                continue
            }
            changes = append(changes, Change{Field: name, Old: oldValue, New: newValue})
            continue
        }

        if true == newPresent {
            changes = append(changes, Change{Field: name, New: newValue})
            continue
        }

        changes = append(changes, Change{Field: name, Old: oldValue})
    }

    return changes
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
