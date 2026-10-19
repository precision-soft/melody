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

/* the encrypted column types are recognised through the marker interface, since each compartment-bound generic instantiation is a distinct reflect.Type an identity list cannot enumerate */
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

/* MarshalJSON emits each side by presence rather than emptiness, so a transition to a zero value renders the zero and is told apart from the one-sided shape of an insert or a delete. A Change built by hand records no presence, and a side is then emitted when it is non-nil. */
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

    /* the visit record starts with the two root pointers, since Go permits `type Node struct { *Node }` and such an embed can lead back to the model itself */
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

/* structValueOf resolves a value to the struct behind it, recording every pointer it walks through, and reports whether it met one twice: an embedded pointer can lead back to a struct the walk is inside, and the unbounded recursion would be a fatal stack overflow no recover catches. */
func structValueOf(value reflect.Value, seen map[embedVisitKey]struct{}, after bool) (reflect.Value, bool) {
    for reflect.Pointer == value.Kind() {
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

/* dereferencePointerType follows a pointer type to what it points at, ending at the first type met twice, since `type Pointer *Pointer` is legal Go and would otherwise spin forever. The type it ends on is a pointer, which every caller reads as neither an encrypted string type nor a struct. */
func dereferencePointerType(pointerType reflect.Type) reflect.Type {
    visited := map[reflect.Type]struct{}{}

    for reflect.Pointer == pointerType.Kind() {
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

/* the visit key pairs the pointer with the slice length plus one, so two re-slices sharing a backing-array start are both walked and a redact-tagged element past the shorter one is not skipped, while a self-referential slice is still a cycle; a pointer or a map keeps length 0 */
type redactVisitKey struct {
    pointer uintptr
    length  uintptr
}

func valueContainsRedactTagReflect(value reflect.Value, seen map[redactVisitKey]struct{}) bool {
    for reflect.Pointer == value.Kind() || reflect.Interface == value.Kind() {
        if true == value.IsNil() {
            return false
        }
        if reflect.Pointer == value.Kind() {
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
        /* guard self-referential slices reached through an interface element (slice -> any -> same slice), which carry no pointer node for the deref loop to catch and would otherwise recurse until the stack overflows. */
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

/* typeContainsRedactTag answers whether a redact tag is reachable from fieldType over a type graph that may be cyclic, recording each type before taking it apart. A type met a second time answers false, which is exact: the frame still exploring it reports any tag it finds, and answering true would redact every self-referential shape. */
func typeContainsRedactTag(fieldType reflect.Type, seen map[reflect.Type]struct{}) bool {
    for reflect.Pointer == fieldType.Kind() || reflect.Slice == fieldType.Kind() || reflect.Array == fieldType.Kind() {
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

        /* an encrypted column type is redactable here as on the value walk, so a struct whose only sensitive member is an EncryptedString field is not read as tag-free */
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
