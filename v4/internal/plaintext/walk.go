package plaintext

import (
    "bytes"
    "encoding"
    "encoding/json"
    "fmt"
    "reflect"
    "sort"
    "strings"
    "unicode"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/internal"
)

const (
    DefaultMaxDepth   = 8
    DefaultMaxLines   = 1024
    DefaultMaxBytes   = 256 * 1024
    DefaultMaxMembers = 16 * 1024
)

/* TruncatedLine ends a body the walk stopped writing because a bound was reached. */
const TruncatedLine = "...(truncated)"

/* Options bounds one walk: how deep it descends, how many lines it writes, how many bytes of text and how many values it spends; an unlimited bound is math.MaxInt. LabelKeys spells every key as words, requestId as request id, for a body whose keys are the framework's own. */
type Options struct {
    MaxDepth   int
    MaxLines   int
    MaxBytes   int
    MaxMembers int
    LabelKeys  bool
}

func DefaultOptions() Options {
    return Options{
        MaxDepth:   DefaultMaxDepth,
        MaxLines:   DefaultMaxLines,
        MaxBytes:   DefaultMaxBytes,
        MaxMembers: DefaultMaxMembers,
    }
}

/* Walk writes values as lines: a map as one key: value line per entry in key order, a struct by its json names, a nested map or struct indented beneath its key, a list one - line per item, a map or list inside a list indented beneath its -. A value deeper than MaxDepth, a value that holds itself among them, is written as an ellipsis, and the walk stops adding lines past MaxLines or once it has spent its bytes or its members, whatever the shape of what it walks. */
type Walk struct {
    options          Options
    lines            []string
    remainingBytes   int
    remainingMembers int
}

/* NewWalk starts a walk after the lines given, which count toward MaxLines. */
func NewWalk(options Options, lines []string) *Walk {
    return &Walk{
        options:          options,
        lines:            lines,
        remainingBytes:   options.MaxBytes,
        remainingMembers: options.MaxMembers,
    }
}

/* Entry writes one entry at the top level and the entries nested under it. */
func (instance *Walk) Entry(key string, value any) {
    instance.entry("", key, reflect.ValueOf(value), 0)
}

/* Lines answers the lines written, cut at MaxLines and ended with TruncatedLine when a bound stopped the walk. */
func (instance *Walk) Lines() []string {
    if instance.options.MaxLines < len(instance.lines) {
        return append(instance.lines[:instance.options.MaxLines], TruncatedLine)
    }

    if true == instance.exhausted() {
        return append(instance.lines, TruncatedLine)
    }

    return instance.lines
}

/* Render writes a value on its own: nil as nothing, a scalar as its text, whole, a map or a struct as its entries and a list as its items, one per line, each line ending with a newline. A method that panics, and a MarshalJSON or MarshalText that fails, panic here; the caller decides what a failed rendering answers. */
func Render(value any, options Options) string {
    walk := NewWalk(options, nil)

    reflected, depth, isNil := walk.resolve(reflect.ValueOf(value), 0)
    if true == isNil {
        return ""
    }

    if text, isText := methodText(reflected); true == isText {
        return text
    }

    if options.MaxDepth <= depth && true == isComposite(reflected) {
        return "..."
    }

    switch layout(reflected) {
    case layoutEntries:
        walk.entries("", reflected, depth)
    case layoutItems:
        walk.items("", reflected, depth)
    default:
        return fmt.Sprint(reflected)
    }

    lines := walk.Lines()
    if 0 == len(lines) {
        return ""
    }

    return strings.Join(lines, "\n") + "\n"
}

type layoutKind uint8

const (
    layoutScalar layoutKind = iota
    layoutEntries
    layoutItems
)

/* layout answers how a value is written: a map or a struct as entries, a list other than a byte sequence as items, anything else as its text */
func layout(reflected reflect.Value) layoutKind {
    switch reflected.Kind() {
    case reflect.Map, reflect.Struct:
        return layoutEntries
    case reflect.Slice, reflect.Array:
        if reflect.Uint8 != reflected.Type().Elem().Kind() {
            return layoutItems
        }
    }

    return layoutScalar
}

/* isComposite answers whether a value can reach another one, so the depth bound writes it as an ellipsis: a map, a struct, a list, or a pointer the walk stopped following at the bound */
func isComposite(reflected reflect.Value) bool {
    switch reflected.Kind() {
    case reflect.Pointer, reflect.Interface:
        return true
    }

    return layoutScalar != layout(reflected)
}

/* methodText answers the text a value gives itself, in the order the walk reads it: an error's message, an encoding.TextMarshaler's text, a fmt.Stringer's text, then a string as it is; a json.Marshaler is read before it, by resolve. The method of a value read through an unexported field is not called. A MarshalText that fails panics with its error. */
func methodText(reflected reflect.Value) (string, bool) {
    if true == reflected.CanInterface() {
        switch typedValue := reflected.Interface().(type) {
        case error:
            return typedValue.Error(), true
        case encoding.TextMarshaler:
            text, marshalErr := typedValue.MarshalText()
            if nil != marshalErr {
                panic(marshalErr)
            }

            return string(text), true
        case fmt.Stringer:
            return typedValue.String(), true
        }
    }

    if reflect.String == reflected.Kind() {
        return reflected.String(), true
    }

    return "", false
}

/* jsonMarshaler answers the marshaler of a value that writes its own json and is not an error, whose message the walk reads first */
func jsonMarshaler(reflected reflect.Value) (json.Marshaler, bool) {
    if false == reflected.CanInterface() {
        return nil, false
    }

    candidate := reflected.Interface()
    if _, isError := candidate.(error); true == isError {
        return nil, false
    }

    marshaler, isMarshaler := candidate.(json.Marshaler)

    return marshaler, isMarshaler
}

/* resolve unwraps a value, and a value that writes its own json is replaced by that json decoded, so a field its MarshalJSON leaves out or redacts is not written; a MarshalJSON that fails panics with its error */
func (instance *Walk) resolve(value reflect.Value, depth int) (reflect.Value, int, bool) {
    reflected, depth, isNil := instance.unwrap(value, depth)
    if true == isNil || instance.options.MaxDepth <= depth {
        return reflected, depth, isNil
    }

    marshaler, isMarshaler := jsonMarshaler(reflected)
    if false == isMarshaler {
        return reflected, depth, false
    }

    encoded, marshalErr := json.Marshal(marshaler)
    if nil != marshalErr {
        panic(marshalErr)
    }

    decoder := json.NewDecoder(bytes.NewReader(encoded))
    decoder.UseNumber()

    var decoded any
    if decodeErr := decoder.Decode(&decoded); nil != decodeErr {
        panic(decodeErr)
    }

    return instance.unwrap(reflect.ValueOf(decoded), depth)
}

/* hasOwnForm answers whether a value writes itself, so the walk stops following a pointer at it */
func hasOwnForm(reflected reflect.Value) bool {
    if false == reflected.CanInterface() {
        return false
    }

    switch reflected.Interface().(type) {
    case error, encoding.TextMarshaler, fmt.Stringer, json.Marshaler:
        return true
    }

    return false
}

/* unwrap follows pointers to the value they hold; a pointer is no level of its own, as the interface a map, a list or a field holds its member in is none, but a chain of more than MaxDepth pointers, one that reaches itself, ends at the depth bound. It answers true for nil. */
func (instance *Walk) unwrap(reflected reflect.Value, depth int) (reflect.Value, int, bool) {
    if true == reflected.IsValid() && reflect.Interface == reflected.Kind() && false == reflected.IsNil() {
        reflected = reflected.Elem()
    }

    for steps := 0; ; steps++ {
        if false == reflected.IsValid() {
            return reflected, depth, true
        }

        switch reflected.Kind() {
        case reflect.Pointer, reflect.Interface:
            if true == reflected.IsNil() {
                return reflected, depth, true
            }

            if true == hasOwnForm(reflected) {
                return reflected, depth, false
            }

            if instance.options.MaxDepth <= steps {
                return reflected, max(depth, instance.options.MaxDepth), false
            }

            reflected = reflected.Elem()
        case reflect.Map, reflect.Slice:
            return reflected, depth, reflected.IsNil()
        default:
            return reflected, depth, false
        }
    }
}

func (instance *Walk) linesFull() bool {
    return instance.options.MaxLines < len(instance.lines)
}

func (instance *Walk) entry(indent string, key string, value reflect.Value, depth int) {
    if true == instance.linesFull() || true == instance.exhausted() {
        return
    }

    instance.count()
    label := indent + instance.spend(instance.label(key)) + ":"

    reflected, depth, isNil := instance.resolve(value, depth)
    if true == isNil {
        instance.lines = append(instance.lines, label)

        return
    }

    if instance.options.MaxDepth <= depth {
        instance.lines = append(instance.lines, labelled(label, "..."))

        return
    }

    if text, isText := methodText(reflected); true == isText {
        instance.lines = append(instance.lines, labelled(label, instance.spend(text)))

        return
    }

    switch layout(reflected) {
    case layoutEntries:
        instance.lines = append(instance.lines, label)
        instance.entries(indent+"  ", reflected, depth+1)
    case layoutItems:
        instance.lines = append(instance.lines, label)
        instance.items(indent+"  ", reflected, depth+1)
    default:
        instance.lines = append(instance.lines, labelled(label, instance.scalarText(reflected)))
    }
}

/* entries writes the members of a map in key order, or the fields of a struct by their json names */
func (instance *Walk) entries(indent string, reflected reflect.Value, depth int) {
    if reflect.Struct == reflected.Kind() {
        for _, field := range jsonFields(reflected.Type()) {
            fieldValue, reachable := fieldByIndex(reflected, field.index)
            if false == reachable || true == field.omitted(fieldValue) {
                continue
            }

            instance.entry(indent, field.name, fieldValue, depth)
        }

        return
    }

    type mapEntry struct {
        key     string
        keyType string
        value   reflect.Value
    }

    mapEntries := make([]mapEntry, 0, reflected.Len())
    iterator := reflected.MapRange()
    for iterator.Next() {
        key := iterator.Key()
        if reflect.Interface == key.Kind() && false == key.IsNil() {
            key = key.Elem()
        }

        mapEntries = append(mapEntries, mapEntry{key: keyText(key), keyType: key.Type().String(), value: iterator.Value()})
    }
    /* two keys of one text, 1 and "1" in a map[any]any, keep one order: their types' */
    sort.SliceStable(mapEntries, func(left int, right int) bool {
        if mapEntries[left].key != mapEntries[right].key {
            return mapEntries[left].key < mapEntries[right].key
        }

        return mapEntries[left].keyType < mapEntries[right].keyType
    })

    for _, mapEntry := range mapEntries {
        instance.entry(indent, mapEntry.key, mapEntry.value, depth)
    }
}

/* items writes one - line per member of a list; a member that is itself a map, a struct or a list is written beneath its - */
func (instance *Walk) items(indent string, reflected reflect.Value, depth int) {
    for index := 0; index < reflected.Len() && false == instance.linesFull() && false == instance.exhausted(); index++ {
        instance.item(indent, reflected.Index(index), depth)
    }
}

func (instance *Walk) item(indent string, value reflect.Value, depth int) {
    instance.count()

    reflected, depth, isNil := instance.resolve(value, depth)
    if true == isNil {
        instance.lines = append(instance.lines, indent+"-")

        return
    }

    if text, isText := methodText(reflected); true == isText {
        instance.lines = append(instance.lines, indent+"- "+instance.spend(text))

        return
    }

    if instance.options.MaxDepth <= depth && true == isComposite(reflected) {
        instance.lines = append(instance.lines, indent+"- ...")

        return
    }

    switch layout(reflected) {
    case layoutEntries:
        instance.lines = append(instance.lines, indent+"-")
        instance.entries(indent+"  ", reflected, depth+1)
    case layoutItems:
        instance.lines = append(instance.lines, indent+"-")
        instance.items(indent+"  ", reflected, depth+1)
    default:
        instance.lines = append(instance.lines, indent+"- "+instance.scalarText(reflected))
    }
}

/* scalarText spells a value that holds no other value: a number, a bool, a byte sequence, a channel or a func; the entry or the item it is written in counted it */
func (instance *Walk) scalarText(reflected reflect.Value) string {
    return instance.spend(fmt.Sprint(reflected))
}

/* keyText spells a map key: a pointer or a channel by its address, whatever its methods, since fmt would print what a pointer reaches, which may hold the map itself, and a nil one's method may panic */
func keyText(key reflect.Value) string {
    switch key.Kind() {
    case reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
        return fmt.Sprintf("0x%x", key.Pointer())
    }

    if text, isText := methodText(key); true == isText {
        return text
    }

    return fmt.Sprint(key)
}

/* labelled writes an entry on its label's line, the bare label when the value is empty */
func labelled(label string, text string) string {
    if "" == text {
        return label
    }

    return label + " " + text
}

func (instance *Walk) label(key string) string {
    if false == instance.options.LabelKeys {
        return key
    }

    return Label(key)
}

/* Label spells a camel-cased key as words: requestId reads request id. */
func Label(key string) string {
    var builder strings.Builder
    for index, character := range key {
        if 0 < index && true == unicode.IsUpper(character) {
            builder.WriteRune(' ')
        }
        builder.WriteRune(unicode.ToLower(character))
    }

    return builder.String()
}

func (instance *Walk) count() {
    instance.remainingMembers--
}

/* spend answers a key or a value written on a line, every control character spelled as its escape, so a value cannot end its line and write an entry of its own */
func (instance *Walk) spend(text string) string {
    return instance.cut(internal.EscapeControlCharacters(text))
}

/* cut answers the text and takes its length out of the budget; a text that does not fit is cut on a rune boundary and ends with an ellipsis, and nothing is answered once the budget is spent */
func (instance *Walk) cut(text string) string {
    if true == instance.exhausted() {
        return "..."
    }

    if len(text) <= instance.remainingBytes {
        instance.remainingBytes -= len(text)

        return text
    }

    cut := instance.remainingBytes
    for 0 < cut && false == utf8.RuneStart(text[cut]) {
        cut--
    }

    instance.remainingBytes = 0

    return text[:cut] + "..."
}

func (instance *Walk) exhausted() bool {
    return 0 >= instance.remainingBytes || 0 >= instance.remainingMembers
}
