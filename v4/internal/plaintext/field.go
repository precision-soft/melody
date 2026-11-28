package plaintext

import (
    "reflect"
    "sort"
    "strings"
    "sync"
)

/* jsonField is one field of a struct as encoding/json writes it: the name a json client reads and the path to the field, through embedded structs */
type jsonField struct {
    name      string
    tagged    bool
    index     []int
    omitEmpty bool
    omitZero  bool
}

var jsonFieldCache sync.Map

/* jsonFields answers the fields encoding/json writes for a struct type, in its order: the exported fields under their json names, a json:"-" field left out, the fields of an untagged embedded struct promoted, and of two promoted fields of one name the shallower, or the one tagged at the same depth, kept and both dropped when that does not decide */
func jsonFields(structType reflect.Type) []jsonField {
    if cached, isCached := jsonFieldCache.Load(structType); true == isCached {
        return cached.([]jsonField)
    }

    fields, _ := jsonFieldCache.LoadOrStore(structType, collectJsonFields(structType))

    return fields.([]jsonField)
}

func collectJsonFields(structType reflect.Type) []jsonField {
    type queued struct {
        structType reflect.Type
        index      []int
    }

    var fields []jsonField
    visited := map[reflect.Type]bool{}
    current := []queued{}
    next := []queued{{structType: structType}}
    count := map[reflect.Type]int{}
    nextCount := map[reflect.Type]int{structType: 1}

    for 0 < len(next) {
        current, next = next, current[:0]
        count, nextCount = nextCount, map[reflect.Type]int{}

        for _, entry := range current {
            if true == visited[entry.structType] {
                continue
            }
            visited[entry.structType] = true

            for fieldIndex := 0; fieldIndex < entry.structType.NumField(); fieldIndex++ {
                structField := entry.structType.Field(fieldIndex)

                fieldType := structField.Type
                if true == structField.Anonymous {
                    if reflect.Pointer == fieldType.Kind() {
                        fieldType = fieldType.Elem()
                    }

                    if false == structField.IsExported() && reflect.Struct != fieldType.Kind() {
                        continue
                    }
                } else if false == structField.IsExported() {
                    continue
                }

                tag := structField.Tag.Get("json")
                if "-" == tag {
                    continue
                }

                name, options, _ := strings.Cut(tag, ",")

                index := make([]int, len(entry.index)+1)
                copy(index, entry.index)
                index[len(entry.index)] = fieldIndex

                if "" != name || false == structField.Anonymous || reflect.Struct != fieldType.Kind() {
                    tagged := "" != name
                    if false == tagged {
                        name = structField.Name
                    }

                    fields = append(fields, jsonField{
                        name:      name,
                        tagged:    tagged,
                        index:     index,
                        omitEmpty: true == hasTagOption(options, "omitempty"),
                        omitZero:  true == hasTagOption(options, "omitzero"),
                    })

                    /* a struct embedded twice at one depth writes each of its fields twice, so dominantFields drops both, as encoding/json does */
                    if 1 < count[entry.structType] {
                        fields = append(fields, fields[len(fields)-1])
                    }

                    continue
                }

                nextCount[fieldType]++
                if 1 == nextCount[fieldType] {
                    next = append(next, queued{structType: fieldType, index: index})
                }
            }
        }
    }

    return dominantFields(fields)
}

func hasTagOption(options string, option string) bool {
    for _, candidate := range strings.Split(options, ",") {
        if option == candidate {
            return true
        }
    }

    return false
}

/* dominantFields keeps, per name, the field encoding/json writes, then answers them in the struct's order */
func dominantFields(fields []jsonField) []jsonField {
    sort.SliceStable(fields, func(left int, right int) bool {
        if fields[left].name != fields[right].name {
            return fields[left].name < fields[right].name
        }

        if len(fields[left].index) != len(fields[right].index) {
            return len(fields[left].index) < len(fields[right].index)
        }

        return true == fields[left].tagged && false == fields[right].tagged
    })

    kept := make([]jsonField, 0, len(fields))
    for start := 0; start < len(fields); {
        end := start + 1
        for end < len(fields) && fields[start].name == fields[end].name {
            end++
        }

        if 1 == end-start {
            kept = append(kept, fields[start])
        } else if len(fields[start].index) < len(fields[start+1].index) || (true == fields[start].tagged && false == fields[start+1].tagged) {
            kept = append(kept, fields[start])
        }

        start = end
    }

    sort.Slice(kept, func(left int, right int) bool {
        leftIndex := kept[left].index
        rightIndex := kept[right].index
        for position := 0; position < len(leftIndex) && position < len(rightIndex); position++ {
            if leftIndex[position] != rightIndex[position] {
                return leftIndex[position] < rightIndex[position]
            }
        }

        return len(leftIndex) < len(rightIndex)
    })

    return kept
}

/* fieldByIndex reads a field through the embedded structs on its path; a nil embedded pointer on the way means the field is not there, as encoding/json reads it */
func fieldByIndex(structValue reflect.Value, index []int) (reflect.Value, bool) {
    reflected := structValue
    for position, fieldIndex := range index {
        if 0 < position && reflect.Pointer == reflected.Kind() {
            if true == reflected.IsNil() {
                return reflect.Value{}, false
            }

            reflected = reflected.Elem()
        }

        reflected = reflected.Field(fieldIndex)
    }

    return reflected, true
}

/* omitted answers whether omitempty or omitzero leaves the value out, as encoding/json decides it */
func (instance jsonField) omitted(value reflect.Value) bool {
    if true == instance.omitEmpty && true == isEmptyValue(value) {
        return true
    }

    if true == instance.omitZero {
        if true == value.CanInterface() {
            if zeroer, isZeroer := value.Interface().(interface{ IsZero() bool }); true == isZeroer {
                if (reflect.Pointer == value.Kind() || reflect.Interface == value.Kind()) && true == value.IsNil() {
                    return true
                }

                return zeroer.IsZero()
            }
        }

        return value.IsZero()
    }

    return false
}

func isEmptyValue(value reflect.Value) bool {
    switch value.Kind() {
    case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
        return 0 == value.Len()
    case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
        reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
        reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
        return true == value.IsZero()
    }

    return false
}
