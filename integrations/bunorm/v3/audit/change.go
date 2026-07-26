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

    /* the embed walk carries a visit record from the root down because Go rejects `type Node struct { Node }` but permits `type Node struct { *Node }`: such an embed can point back at a struct the walk is already inside, and that loop has no nil for the pointer chase to stop on. Seeding the record with the two root pointers means the first embed leading back to the model itself is recognised as the cycle it is, instead of replaying the model's own fields one level down before the guard catches it. */
    seen := map[embedVisitKey]struct{}{}

    collectChanges(&changes, structValue(before, seen, false), structValue(after, seen, true), ignore, false, seen)

    /* an empty result is returned as an empty slice, never a nil one: a nil slice marshals to the JSON literal null in the changes column, where a consumer reading it as an array errors out and cannot tell it from a genuinely absent change-set. An idempotent update and any non-struct model both land here. */
    if 0 == len(changes) {
        return []Change{}
    }

    return changes
}

/* the before and after graphs are keyed apart so a pointer the two sides share — the ordinary shape for an embed the statement did not touch — is not mistaken for a cycle and dropped from the change-set */
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

            /* an embed leading back to a struct the walk is already inside contributes nothing: the frame that first reached that struct reports its fields, and descending again would repeat them until the stack is gone. Either side closing the loop drops the whole embed, because walking the other side alone would read as fields appearing or vanishing when nothing about them changed. */
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

    embedded := dereferencePointerType(field.Type)

    if reflect.Struct != embedded.Kind() {
        return false
    }

    return baseModelType != embedded && encryptedStringType != embedded && encryptedDeterministicStringType != embedded && reflect.TypeOf(time.Time{}) != embedded
}

/* structValueOf resolves a value to the struct behind it and records every pointer it walks through, reporting as its second result whether the chase met a pointer this walk had already been through. Go permits `type Node struct { *Node }` where it rejects the non-pointer form, so an embedded pointer can lead back to a struct the walk is already inside; that loop carries no nil to end the chase and, unrecorded, the embed recursion runs until the stack is gone — a fatal error, not a panic, so nothing downstream can recover it. */
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

/* dereferencePointerType follows a pointer type down to what it ultimately points at. The chase is not guaranteed to reach a non-pointer: `type Pointer *Pointer` is legal Go and its element is itself, so an unrecorded loop spins at full processor forever, which is a hang no recover and no request timeout can undo. The first type met twice ends the chase and is returned as-is; it is a pointer type, so every caller reads it as "not one of the encrypted string types" and "not a struct", which is the same answer the chase would have produced had it been able to finish. */
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

    if fieldType == encryptedStringType || fieldType == encryptedDeterministicStringType {
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

/* @important the visit key combines the pointer with a length discriminator so two distinct re-slices that share a backing-array start (full and full[:1]) are not collapsed into the same already-visited value — that false dedup would skip a redact-tagged element living past the shorter slice's length and leak it as plaintext. A *T or map header is identified by its pointer alone (length stays 0); a slice adds its length + 1, so a self-referential slice (same pointer, same length) is still caught as a cycle while a different-length view of the same array is traversed, and a slice key never collides with a length-0 pointer/map key. */
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
    if valueType == encryptedStringType || valueType == encryptedDeterministicStringType {
        return true
    }

    switch value.Kind() {
    case reflect.Slice:
        /* @important guard self-referential slices reached through an interface element (slice -> any -> same slice), which carry no pointer node for the deref loop to catch and would otherwise recurse until the stack overflows. */
        pointer := value.Pointer()
        if 0 != pointer {
            /* @important key on pointer AND length so a re-slice sharing the backing-array start but with a different length (full vs full[:1]) is not mistaken for an already-visited value, which would skip a redact-tagged element past the shorter view and leak it */
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

            subFieldType := dereferencePointerType(subField.Type)
            if subFieldType == encryptedStringType || subFieldType == encryptedDeterministicStringType {
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

/* typeContainsRedactTag answers whether a redact tag is reachable from fieldType. The type graph it walks is finite but freely cyclic — `type Attributes map[string]Attributes`, `type Node []Node` and `type Pointer *Pointer` are all legal Go, as are the same shapes spread across two named types — so every type is recorded before it is taken apart, both in the element chase and in the recursion, and a type met a second time ends that branch.

Ending it with false is the exact answer rather than a concession. The walk is a reachability question whose result is an OR over the fields, and the first true returns straight out through every frame; a type already under examination can therefore only be reached from a frame that is still exploring it and will report any tag it finds on its own. False here means no redact tag is reachable by any path, and the alternative — answering true for a back-edge — would redact every self-referential shape, `type Category struct { Children []Category }` included, turning the audit trail into a column of placeholders. */
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
