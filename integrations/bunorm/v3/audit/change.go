package audit

import (
    "encoding/json"
    "reflect"
    "strconv"
    "strings"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

const redactedValue = "<redacted>"

/* each compartment-bound generic instantiation is a distinct reflect.Type, so only the marker interface recognises them all */
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

/* MarshalJSON emits each side by presence, so a change to a zero value is told apart from an insert or a delete */
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

    /* the root pointers are seen from the start, since `type Node struct { *Node }` can lead back to the model */
    seen := map[embedVisitKey]struct{}{}

    collectChanges(&changes, structValue(before, seen, false), structValue(after, seen, true), ignore, false, seen, nil, nil)

    /* never nil: a nil slice marshals to null, which a reader of the changes array cannot tell from an absent change set */
    if 0 == len(changes) {
        return []Change{}
    }

    return changes
}

/* keyed apart, so a pointer both sides share is not mistaken for a cycle */
type embedVisitKey struct {
    pointer uintptr
    after   bool
}

func collectChanges(changes *[]Change, beforeValue reflect.Value, afterValue reflect.Value, ignore map[string]struct{}, forceRedact bool, seen map[embedVisitKey]struct{}, rowPaths map[string]struct{}, path []int) {
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

    if nil == rowPaths {
        rowPaths = embeddedRowFieldPaths(structType)
    }

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

            /* an embed closing a loop on either side adds nothing: the frame that first reached the struct reports its fields */
            if true == beforeCycle || true == afterCycle {
                continue
            }

            embedRedact := forceRedact || "redact" == field.Tag.Get("audit")
            collectChanges(changes, embeddedBefore, embeddedAfter, ignore, embedRedact, seen, rowPaths, appendIndex(path, index))
            continue
        }

        name, skip := auditFieldName(field)
        if true == skip {
            continue
        }

        /* an embed: struct is walked as one value here, while bun spreads it into prefixed columns that are never the row paths */
        if _, spread := parseBunTagOptions(field)["embed"]; 0 < len(path) && false == spread {
            if _, inRow := rowPaths[indexPathKey(appendIndex(path, index))]; false == inRow {
                continue
            }
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

/* structValueOf reports a pointer met twice, since an embed leading back would overflow the stack past any recover */
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

/* dereferencePointerType stops at the first type met twice, since `type Pointer *Pointer` is legal Go */
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

/* the slice length is part of the key, so two re-slices of one backing array are both walked */
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
        /* a slice reached through an interface element carries no pointer for the dereference loop to catch */
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

/* typeContainsRedactTag answers false for a type met twice: the frame still exploring it reports any tag it finds */
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

        /* an encrypted column type is redactable on the type walk as on the value walk */
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

    /* a scan-only field is never written by an update and a relation is not a column, so neither is part of the row's change set */
    _, options := parseBunTag(tag)
    for _, option := range []string{"scanonly", "rel", "m2m"} {
        if _, has := options[option]; true == has {
            return "", true
        }
    }

    first := tag
    if comma := strings.IndexByte(tag, ','); -1 != comma {
        first = tag[:comma]
    }

    if "" == first {
        return field.Name, false
    }

    return first, false
}

/* the leading token is the column unless it carries a value, and every later token is an option, as bun's tag parser reads them */
func parseBunTag(tag string) (string, map[string]string) {
    name := ""
    options := map[string]string{}

    for position, token := range strings.Split(tag, ",") {
        key, value, hasValue := strings.Cut(token, ":")
        if 0 == position && false == hasValue {
            name = token

            continue
        }

        options[key] = value
    }

    return name, options
}

func parseBunTagOptions(field reflect.StructField) map[string]string {
    _, options := parseBunTag(field.Tag.Get("bun"))

    return options
}

type bunRowField struct {
    index  []int
    column string
    tagged bool
}

/* bun keeps an embedded field only when no other field of the level carries its column, or when it is the one tagged field that does */
func embeddedRowFieldPaths(structType reflect.Type) map[string]struct{} {
    paths := map[string]struct{}{}

    for _, field := range bunRowFields(structType, map[reflect.Type]struct{}{}) {
        if 1 < len(field.index) {
            paths[indexPathKey(field.index)] = struct{}{}
        }
    }

    return paths
}

func bunRowFields(structType reflect.Type, visiting map[reflect.Type]struct{}) []bunRowField {
    if _, inside := visiting[structType]; true == inside {
        return nil
    }
    visiting[structType] = struct{}{}
    defer delete(visiting, structType)

    levelColumns := map[string]struct{}{}
    var own []bunRowField
    var embedded []bunRowField

    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)
        tag := field.Tag.Get("bun")

        if "-" == tag {
            levelColumns[field.Name] = struct{}{}

            continue
        }

        if false == field.IsExported() && false == field.Anonymous {
            continue
        }

        name, options := parseBunTag(tag)

        prefix, hasPrefix := options["embed"]
        if true == field.Anonymous {
            prefix = ""
        }

        if true == field.Anonymous || true == hasPrefix {
            embeddedType := dereferencePointerType(field.Type)
            if reflect.Struct != embeddedType.Kind() {
                continue
            }

            for _, subfield := range bunRowFields(embeddedType, visiting) {
                embedded = append(embedded, bunRowField{
                    index:  append([]int{index}, subfield.index...),
                    column: prefix + subfield.column,
                    tagged: subfield.tagged,
                })
            }

            continue
        }

        column := bunColumnName(field.Name)
        if "" != name {
            column = name
        }
        if explicit, has := options["column"]; true == has {
            column = explicit
        }

        levelColumns[column] = struct{}{}
        own = append(own, bunRowField{index: []int{index}, column: column, tagged: "" != name || 0 < len(options)})
    }

    holders := map[string]int{}
    taggedHolders := map[string]int{}
    for column := range levelColumns {
        holders[column]++
        taggedHolders[column]++
    }
    for _, field := range embedded {
        holders[field.column]++
        if true == field.tagged {
            taggedHolders[field.column]++
        }
    }

    for _, field := range embedded {
        if 1 < holders[field.column] && false == (true == field.tagged && 1 == taggedHolders[field.column]) {
            continue
        }

        own = append(own, field)
    }

    return own
}

/* bun's own name for an untagged column: an upper-case letter next to a lower-case one starts a new word */
func bunColumnName(goName string) string {
    column := make([]byte, 0, len(goName)+5)

    for position := 0; position < len(goName); position++ {
        character := goName[position]
        if 'A' > character || 'Z' < character {
            column = append(column, character)

            continue
        }

        lower := character + 32
        if 0 < position && position+1 < len(goName) && (isLowerAscii(goName[position-1]) || isLowerAscii(goName[position+1])) {
            column = append(column, '_', lower)

            continue
        }

        column = append(column, lower)
    }

    return string(column)
}

func isLowerAscii(character byte) bool {
    return 'a' <= character && 'z' >= character
}

func appendIndex(path []int, index int) []int {
    extended := make([]int, len(path), len(path)+1)
    copy(extended, path)

    return append(extended, index)
}

func indexPathKey(index []int) string {
    parts := make([]string, len(index))
    for position, value := range index {
        parts[position] = strconv.Itoa(value)
    }

    return strings.Join(parts, ".")
}
