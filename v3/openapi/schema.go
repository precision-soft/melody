package openapi

import (
    "fmt"
    "reflect"
    "regexp"
    "strconv"
    "strings"
    "time"
)

var timeType = reflect.TypeOf(time.Time{})

func schemaFromType(targetType reflect.Type, components map[string]*Schema, names map[reflect.Type]string) *Schema {
    return buildSchema(targetType, components, names, make(map[reflect.Type]bool))
}

func buildSchema(targetType reflect.Type, components map[string]*Schema, names map[reflect.Type]string, visited map[reflect.Type]bool) *Schema {
    if nil == targetType {
        return &Schema{}
    }

    nullable := false
    for reflect.Ptr == targetType.Kind() {
        nullable = true
        targetType = targetType.Elem()
    }

    if targetType == timeType {
        return withNullable(&Schema{Type: "string", Format: "date-time"}, nullable)
    }

    switch targetType.Kind() {
    case reflect.String:
        return withNullable(&Schema{Type: "string"}, nullable)
    case reflect.Bool:
        return withNullable(&Schema{Type: "boolean"}, nullable)
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
        reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        return withNullable(&Schema{Type: "integer"}, nullable)
    case reflect.Float32, reflect.Float64:
        return withNullable(&Schema{Type: "number"}, nullable)
    case reflect.Slice, reflect.Array:
        if reflect.Slice == targetType.Kind() && reflect.Uint8 == targetType.Elem().Kind() {
            return withNullable(&Schema{Type: "string", Format: "byte"}, nullable)
        }

        return withNullable(&Schema{Type: "array", Items: elementSchema(targetType.Elem(), components, names, visited)}, nullable)
    case reflect.Map:
        return withNullable(&Schema{Type: "object", AdditionalProperties: elementSchema(targetType.Elem(), components, names, visited)}, nullable)
    case reflect.Struct:
        return structSchemaReference(targetType, components, names, visited, nullable)
    default:
        return withNullable(&Schema{}, nullable)
    }
}

/* @important a collection element carries no validate tag of its own — a tag on the field constrains the collection, not each entry — so the unsigned floor is stamped here rather than left to addFieldProperty, which only ever sees the field. Without it the items schema of a []uint advertises the whole negative range the decoder rejects. The absence of a tag is also what makes the floor safe here: there is no validator-placed exclusive bound for it to weaken. */
func elementSchema(elementType reflect.Type, components map[string]*Schema, names map[reflect.Type]string, visited map[reflect.Type]bool) *Schema {
    schema := buildSchema(elementType, components, names, visited)

    applyUnsignedLowerBound(schema, elementType)

    return schema
}

func structSchemaReference(structType reflect.Type, components map[string]*Schema, names map[reflect.Type]string, visited map[reflect.Type]bool, nullable bool) *Schema {
    if "" == structType.Name() {
        return withNullable(buildStructSchema(structType, components, names, visited), nullable)
    }

    name := schemaComponentName(structType, names)
    if _, built := components[name]; false == built {
        components[name] = &Schema{Type: "object"}
        components[name] = buildStructSchema(structType, components, names, visited)
    }

    return withNullable(&Schema{Ref: "#/components/schemas/" + name}, nullable)
}

func schemaComponentName(structType reflect.Type, names map[reflect.Type]string) string {
    if existing, assigned := names[structType]; true == assigned {
        return existing
    }

    base := structType.Name()
    candidate := base
    suffix := 2
    for true == componentNameInUse(candidate, names) {
        candidate = base + strconv.Itoa(suffix)
        suffix++
    }

    names[structType] = candidate

    return candidate
}

func componentNameInUse(candidate string, names map[reflect.Type]string) bool {
    for _, assigned := range names {
        if assigned == candidate {
            return true
        }
    }

    return false
}

func buildStructSchema(structType reflect.Type, components map[string]*Schema, names map[reflect.Type]string, visited map[reflect.Type]bool) *Schema {
    if true == visited[structType] {
        return &Schema{Type: "object"}
    }

    visited[structType] = true
    defer delete(visited, structType)

    schema := &Schema{
        Type:       "object",
        Properties: make(map[string]*Schema),
    }

    var required []string
    rejectsAll := false
    collectStructFields(structType, components, names, visited, schema.Properties, &required, &rejectsAll)

    if 0 == len(schema.Properties) {
        schema.Properties = nil
    }

    if 0 < len(required) {
        schema.Required = required
    }

    if true == rejectsAll {
        markFieldUnsatisfiable(schema)
    }

    return schema
}

/* @important the validator runs a promoted embed's own validate tag against the embed value itself (validation/validator.go), so a tag no value satisfies — malformed syntax, unconsumable parameters, notEmpty/greaterThan/lessThan (a struct value falls into their reject branch and a nil embed pointer is rejected as null), a malformed numeric bound, a parseable max below the value embed's stringification floor — rejects every payload of the enclosing object; the promoted properties alone cannot express that, so the parent schema is contradicted instead. notBlank on a pointer embed (rejects only the all-promoted-properties-absent payload) has no cheap object-level advertisement and is left unmirrored. A reject-all tag on an embed reached only through a pointer embed at a deeper level is a related over-approximation: the object is advertised unsatisfiable, while the validator skips the tag for a payload that never materialises the intervening pointer, so the spec is stricter than the server (fail-closed). Suppressing it would advertise the promoted properties as satisfiable when a payload that reaches them is always rejected, so the safe over-approximation stands rather than a faithless one. */
func embedTagRejectsAll(field reflect.StructField) bool {
    validateTag := field.Tag.Get("validate")

    return true == tagHasInvalidSyntax(validateTag) ||
        true == tagHasUnconsumedParameterizedParams(validateTag) ||
        true == tagHasParamsOnNonParameterizable(validateTag) ||
        true == tagRejectsStruct(validateTag) ||
        true == tagRejectsAllViaNumericBound(validateTag) ||
        true == valueEmbedMaxBoundRejectsAll(field, validateTag)
}

var embedFormatterInterfaceType = reflect.TypeOf((*fmt.Formatter)(nil)).Elem()
var embedErrorInterfaceType = reflect.TypeOf((*error)(nil)).Elem()
var embedStringerInterfaceType = reflect.TypeOf((*fmt.Stringer)(nil)).Elem()

/* @important MaxLength stringifies the embed value with %v, and a struct rendered by fmt's default verb always carries its braces — the empty struct prints "{}" — so a parseable max below 2 on a VALUE embed rejects every payload of the enclosing object. A pointer embed stays satisfiable (a nil embed passes MaxLength through dereferenceValue ok=false), and a type whose VALUE method set carries Formatter/error/Stringer delegates %v (fmt consults them in that order), voiding the brace floor — a pointer-receiver String() stays out of the value's method set, so such an embed keeps the floor. */
func valueEmbedMaxBoundRejectsAll(field reflect.StructField, validateTag string) bool {
    if reflect.Struct != field.Type.Kind() {
        return false
    }

    if true == field.Type.Implements(embedFormatterInterfaceType) ||
        true == field.Type.Implements(embedErrorInterfaceType) ||
        true == field.Type.Implements(embedStringerInterfaceType) {
        return false
    }

    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
        if "max" != name {
            continue
        }

        valueString, exists := params["value"]
        if false == exists {
            continue
        }

        if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk && 2 > parsed {
            return true
        }
    }

    return false
}

type embeddedCandidate struct {
    field reflect.StructField
}

func collectStructFields(
    structType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    properties map[string]*Schema,
    required *[]string,
    rejectsAll *bool,
) {
    resolved := make(map[string]bool)

    embeddedSeen := make(map[reflect.Type]bool)
    embeddedSeen[structType] = true

    /* @info embedCount tracks how many equal-depth paths reach an embedded type, mirroring encoding/json: a type reached via N paths has its fields duplicated N times so a diamond annihilates in dominantEmbeddedField. */
    embedCount := make(map[reflect.Type]int)

    ownCandidatesByName := make(map[string][]embeddedCandidate)
    var ownOrder []string
    var embedQueue []reflect.Type
    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if true == isPromotedEmbed(field) {
            /* an unexported embed's value cannot pass through Interface, so the validator leaves its tag unenforced (validation/validator.go) and the mirror must too */
            if true == field.IsExported() && true == embedTagRejectsAll(field) {
                *rejectsAll = true
            }

            embeddedType := dereferencedStructType(field.Type)
            if true == embeddedSeen[embeddedType] {
                continue
            }
            if 0 == embedCount[embeddedType] {
                embedQueue = append(embedQueue, embeddedType)
            }
            if 2 > embedCount[embeddedType] {
                embedCount[embeddedType]++
            }
            continue
        }

        if false == field.IsExported() {
            continue
        }

        jsonName, omit := jsonFieldName(field)
        if true == omit {
            continue
        }

        if _, seen := ownCandidatesByName[jsonName]; false == seen {
            ownOrder = append(ownOrder, jsonName)
        }
        ownCandidatesByName[jsonName] = append(ownCandidatesByName[jsonName], embeddedCandidate{field: field})
    }

    for _, jsonName := range ownOrder {
        resolved[jsonName] = true

        winner, ok := dominantEmbeddedField(ownCandidatesByName[jsonName])
        if false == ok {
            continue
        }

        addFieldProperty(winner, jsonName, components, names, visited, properties, required)
    }

    for 0 < len(embedQueue) {
        candidatesByName := make(map[string][]embeddedCandidate)
        var order []string
        var nextLevel []reflect.Type
        nextCount := make(map[reflect.Type]int)

        for _, embeddedType := range embedQueue {
            if true == embeddedSeen[embeddedType] {
                continue
            }
            embeddedSeen[embeddedType] = true

            multiplicity := embedCount[embeddedType]
            if multiplicity < 1 {
                multiplicity = 1
            }

            for index := 0; index < embeddedType.NumField(); index++ {
                field := embeddedType.Field(index)

                if true == isPromotedEmbed(field) {
                    if true == field.IsExported() && true == embedTagRejectsAll(field) {
                        *rejectsAll = true
                    }

                    childType := dereferencedStructType(field.Type)
                    if true == embeddedSeen[childType] {
                        continue
                    }
                    if 0 == nextCount[childType] {
                        nextLevel = append(nextLevel, childType)
                    }
                    /* two copies decide every dominance tie, so the count is capped there instead of doubling through stacked diamonds */
                    nextCount[childType] += multiplicity
                    if 2 < nextCount[childType] {
                        nextCount[childType] = 2
                    }
                    continue
                }

                if false == field.IsExported() {
                    continue
                }

                jsonName, omit := jsonFieldName(field)
                if true == omit {
                    continue
                }

                if true == resolved[jsonName] {
                    continue
                }

                if _, seen := candidatesByName[jsonName]; false == seen {
                    order = append(order, jsonName)
                }
                for copyIndex := 0; copyIndex < multiplicity; copyIndex++ {
                    candidatesByName[jsonName] = append(candidatesByName[jsonName], embeddedCandidate{field: field})
                }
            }
        }

        for _, jsonName := range order {
            resolved[jsonName] = true

            winner, ok := dominantEmbeddedField(candidatesByName[jsonName])
            if false == ok {
                continue
            }

            addFieldProperty(winner, jsonName, components, names, visited, properties, required)
        }

        embedQueue = nextLevel
        embedCount = nextCount
    }
}

func addFieldProperty(
    field reflect.StructField,
    jsonName string,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    properties map[string]*Schema,
    required *[]string,
) {
    propertySchema := buildSchema(field.Type, components, names, visited)
    applyValidation(propertySchema, field.Tag.Get("validate"))

    /* a fixed-length array carries the length its type fixes, so notEmpty's minItems floor is vacuous for it — the validator measures a length that can never fall below it; drop the floor the notEmpty branch stamped so the spec accepts what the server accepts. A schema another rule already contradicted keeps its floor: the maxItems 0 beside it is the other half of that contradiction, and dropping the floor alone would leave the empty array advertised as valid while the validator rejects every payload. */
    if true == fixedArrayNotEmptyIsVacuous(field) && nil == propertySchema.MaxItems {
        propertySchema.MinItems = nil
    }

    /* @important the required and validation decisions run against the bare scalar schema first: the validator sees the DECODED value, so its rules and the absence semantics belong to the scalar, while the quoted advertisement below only changes how the payload spells it */
    fieldRequired := true == isRequired(field, propertySchema) || true == pointerBoundRequiresPresence(field) || true == zeroValueRejectsAbsentProperty(field, propertySchema)

    if quotedKind, isQuoted := jsonStringOptionKind(field); true == isQuoted {
        quotedFieldIsPointer := "" == field.Type.Name() && reflect.Ptr == field.Type.Kind()
        propertySchema = quotedScalarSchema(propertySchema, quotedKind, quotedFieldIsPointer)
    } else {
        applyUnsignedLowerBound(propertySchema, field.Type)
    }

    properties[jsonName] = propertySchema

    if true == fieldRequired {
        *required = append(*required, jsonName)
    }
}

/* @important an unsigned field cannot decode a negative number, so the validator never sees one, yet a kind-blind integer schema advertises the whole negative range as valid; stamp a zero floor so the spec rejects what the decoder rejects. The floor is raise-only and never weakens a bound the validator already placed at or above zero — a value-less greaterThan leaves its exclusive zero minimum intact. */
func applyUnsignedLowerBound(schema *Schema, fieldType reflect.Type) {
    for reflect.Ptr == fieldType.Kind() {
        fieldType = fieldType.Elem()
    }

    switch fieldType.Kind() {
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        if "integer" != schema.Type {
            return
        }

        if nil != schema.Minimum && 0 <= *schema.Minimum {
            return
        }

        floor := float64(0)
        schema.Minimum = &floor
        schema.ExclusiveMinimum = nil
    }
}

/* @important encoding/json honours the ",string" option only on integer, floating point and boolean fields: the payload value is the quoted textual form and a bare scalar errors at decode, so the property must be advertised as the string the decoder actually accepts. A string field's ",string" (double-encoded) form has no faithful schema and is left alone. */
func jsonStringOptionKind(field reflect.StructField) (reflect.Kind, bool) {
    tag := field.Tag.Get("json")
    if "" == tag || "-" == tag {
        return 0, false
    }

    hasStringOption := false
    for _, option := range strings.Split(tag, ",")[1:] {
        if "string" == option {
            hasStringOption = true
        }
    }

    if false == hasStringOption {
        return 0, false
    }

    /* encoding/json dereferences exactly one unnamed pointer before deciding the option applies; a double pointer or a named pointer type keeps its bare form and the option is ignored */
    fieldType := field.Type
    if "" == fieldType.Name() && reflect.Ptr == fieldType.Kind() {
        fieldType = fieldType.Elem()
    }

    switch fieldType.Kind() {
    case reflect.Bool,
        reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
        reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
        reflect.Float32, reflect.Float64:
        return fieldType.Kind(), true
    }

    return 0, false
}

/* @important quotedScalarSchema advertises the textual form the ",string" decoder accepts. The decoder guards only the first character and then hands the literal to strconv, so an integer target takes an optional minus and base-10 digits (leading zeros included, no fraction or exponent) and a boolean takes the two literals; the literal "null", which literalStore consumes as the quoted spelling of null (the target keeps its zero value, a pointer is nilled), is advertised only where the validator accepts that decoded outcome. A float target's ParseFloat grammar is wider than the JSON number grammar (hex floats, trailing dot), so no pattern is advertised for it — under-constraining never contradicts the validator — but a withdrawn null is still excluded through not. The nullable advertisement carries over; an unsatisfiable scalar stays unsatisfiable in its string form (a kind-aware check extends this to an unsigned target under a non-positive exclusive ceiling), except that a NULLABLE unsatisfiable one accepts exactly the null spellings; any other numeric facet has no string equivalent and is dropped. */
func quotedScalarSchema(original *Schema, valueKind reflect.Kind, isPointer bool) *Schema {
    quoted := &Schema{Type: "string", Nullable: original.Nullable}

    if true == scalarSchemaRejectsAll(original) {
        /* a nullable empty value space rejects every non-null value while null itself stays valid, so the quoted form accepts exactly the null literal (the bare null spelling rides on Nullable) */
        if true == original.Nullable {
            quoted.Enum = &[]any{"null"}

            return quoted
        }

        applyEmptyValueSpace(quoted)

        return quoted
    }

    /* an unsigned target under a negative ceiling — or an exclusive one at zero — has an empty value space the kind-blind window check cannot see */
    switch valueKind {
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        if nil != original.Maximum {
            if 0 > *original.Maximum {
                applyEmptyValueSpace(quoted)

                return quoted
            }

            if 0 == *original.Maximum && nil != original.ExclusiveMaximum && true == *original.ExclusiveMaximum {
                applyEmptyValueSpace(quoted)

                return quoted
            }
        }
    }

    nullAccepted := quotedNullAccepted(original, isPointer)

    switch valueKind {
    case reflect.Bool:
        if true == nullAccepted {
            quoted.Enum = &[]any{"true", "false", "null"}
        } else {
            quoted.Enum = &[]any{"true", "false"}
        }
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        if true == nullAccepted {
            quoted.Pattern = "^(-?[0-9]+|null)$"
        } else {
            quoted.Pattern = "^-?[0-9]+$"
        }
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        if true == nullAccepted {
            quoted.Pattern = "^([0-9]+|null)$"
        } else {
            quoted.Pattern = "^[0-9]+$"
        }
    case reflect.Float32, reflect.Float64:
        if false == nullAccepted {
            quoted.Not = &Schema{Enum: &[]any{"null"}}
        }
    }

    return quoted
}

/* @important the quoted "null" decodes to null on a pointer target and to the zero value on a bare scalar, so its advertisement follows what the validator then accepts: the pointer case is exactly the Nullable flag applyValidation maintains (a null-rejecting rule clears it), and the bare case holds while the advertised window still admits zero — a floor above zero or a ceiling below it withdraws the spelling. */
func quotedNullAccepted(original *Schema, isPointer bool) bool {
    if true == isPointer {
        return original.Nullable
    }

    if nil != original.Minimum {
        if 0 < *original.Minimum {
            return false
        }

        if 0 == *original.Minimum && nil != original.ExclusiveMinimum && true == *original.ExclusiveMinimum {
            return false
        }
    }

    if nil != original.Maximum {
        if 0 > *original.Maximum {
            return false
        }

        if 0 == *original.Maximum && nil != original.ExclusiveMaximum && true == *original.ExclusiveMaximum {
            return false
        }
    }

    return true
}

/* @important recognizes the shapes applyEmptyValueSpace stamps on a scalar (the empty exclusive number window, the contradictory boolean enums) plus a genuinely empty greaterThan/lessThan window — for an integer including the adjacent-bounds window no whole number fits, such as the open interval (5, 6). */
func scalarSchemaRejectsAll(schema *Schema) bool {
    if "integer" == schema.Type || "number" == schema.Type {
        if nil == schema.Minimum || nil == schema.Maximum ||
            nil == schema.ExclusiveMinimum || false == *schema.ExclusiveMinimum ||
            nil == schema.ExclusiveMaximum || false == *schema.ExclusiveMaximum {
            return false
        }

        if "integer" == schema.Type {
            return *schema.Maximum-*schema.Minimum <= 1
        }

        return *schema.Minimum >= *schema.Maximum
    }

    if "boolean" == schema.Type && 2 == len(schema.AllOf) {
        return nil != schema.AllOf[0].Enum && nil != schema.AllOf[1].Enum
    }

    return false
}

/* @important validateInternal validates every tagged field even when the request omits the property (v3/validation/validator.go), so the Go zero value is checked against the field's constraints; a constraint the zero value fails turns an omitted property into a 400. The spec must then list the field required, exactly as notBlank/notEmpty (isRequired) and pointer greaterThan/lessThan (pointerBoundRequiresPresence) already do. This covers the remaining absent->zero->reject cases the validator enforces: a min floor of at least 1 on a genuine string rejects "" (the value-less min default is 1), a non-pointer greaterThan bound >= 0 rejects the numeric zero, and a non-pointer lessThan bound <= 0 rejects it too. A min of 0, a negative greaterThan bound, or a positive lessThan bound admits the zero value, so the field stays optional; a malformed bound is left to the unsatisfiable-field handling and not treated as required here. */
func zeroValueRejectsAbsentProperty(field reflect.StructField, schema *Schema) bool {
    isPointer := reflect.Ptr == field.Type.Kind()

    for _, rule := range splitRules(field.Tag.Get("validate")) {
        name, params := splitRule(rule)

        switch name {
        case "min":
            /* a pointer field has no zero value the validator would measure: an omitted property leaves it nil and the length constraint dereferences nothing, so the absence is accepted and the field must stay optional */
            if true == isPointer || "string" != schema.Type || true == isStructuralStringFormat(schema.Format) {
                continue
            }
            bound := 1
            if valueString, exists := params["value"]; true == exists {
                parsed, parsedOk := parseLeadingInt(valueString)
                if false == parsedOk {
                    continue
                }
                bound = parsed
            }
            if bound >= 1 {
                return true
            }
        case "greaterThan":
            if true == isPointer || ("integer" != schema.Type && "number" != schema.Type) {
                continue
            }
            bound := 0
            if valueString, exists := params["value"]; true == exists {
                parsed, parsedOk := parseLeadingInt(valueString)
                if false == parsedOk {
                    continue
                }
                bound = parsed
            }
            if bound >= 0 {
                return true
            }
        case "lessThan":
            if true == isPointer || ("integer" != schema.Type && "number" != schema.Type) {
                continue
            }
            bound := 0
            if valueString, exists := params["value"]; true == exists {
                parsed, parsedOk := parseLeadingInt(valueString)
                if false == parsedOk {
                    continue
                }
                bound = parsed
            }
            if bound <= 0 {
                return true
            }
        }
    }

    return false
}

func pointerBoundRequiresPresence(field reflect.StructField) bool {
    if reflect.Ptr != field.Type.Kind() {
        return false
    }

    for _, rule := range splitRules(field.Tag.Get("validate")) {
        name, _ := splitRule(rule)
        if "greaterThan" == name || "lessThan" == name {
            return true
        }
    }

    return false
}

func dominantEmbeddedField(group []embeddedCandidate) (reflect.StructField, bool) {
    if 1 == len(group) {
        return group[0].field, true
    }

    taggedIndex := -1
    taggedCount := 0
    for index, candidate := range group {
        if true == hasExplicitJsonName(candidate.field) {
            taggedCount++
            taggedIndex = index
        }
    }

    if 1 == taggedCount {
        return group[taggedIndex].field, true
    }

    return reflect.StructField{}, false
}

func dereferencedStructType(targetType reflect.Type) reflect.Type {
    for reflect.Ptr == targetType.Kind() {
        targetType = targetType.Elem()
    }

    return targetType
}

func hasExplicitJsonName(field reflect.StructField) bool {
    tag := field.Tag.Get("json")
    if "" == tag {
        return false
    }

    parts := strings.Split(tag, ",")

    return "" != parts[0] && "-" != parts[0]
}

func isPromotedEmbed(field reflect.StructField) bool {
    if false == field.Anonymous {
        return false
    }

    tag := field.Tag.Get("json")
    if "-" == tag {
        return false
    }

    if "" != tag && "" != strings.Split(tag, ",")[0] {
        return false
    }

    embedded := field.Type
    for reflect.Ptr == embedded.Kind() {
        embedded = embedded.Elem()
    }

    return reflect.Struct == embedded.Kind() && embedded != timeType
}

func withNullable(schema *Schema, nullable bool) *Schema {
    if false == nullable {
        return schema
    }

    if "" != schema.Ref {
        return &Schema{
            AllOf:    []*Schema{{Ref: schema.Ref}},
            Nullable: true,
        }
    }

    schema.Nullable = true

    return schema
}

func jsonFieldName(field reflect.StructField) (string, bool) {
    tag := field.Tag.Get("json")
    if "-" == tag {
        return "", true
    }

    if "" == tag {
        return field.Name, false
    }

    parts := strings.Split(tag, ",")
    if "" == parts[0] {
        return field.Name, false
    }

    return parts[0], false
}

/* @important isRequired lists a field required only when the validator actually turns its absence into a 400. notEmpty rejects every absent zero value (empty string/slice/map, and any other kind outright). notBlank rejects an absent value only for a pointer (nil dereferences to nothing) or a genuine string (the zero "" is blank); on the other kinds the zero value stringifies non-blank ("0", "false", "[]", the zero time) and passes, so listing those required would forbid an omission the validator accepts. */
func isRequired(field reflect.StructField, schema *Schema) bool {
    /* an omitted pointer or interface field dereferences to nothing, which notBlank rejects as "this field is required" */
    absenceIsNil := reflect.Ptr == field.Type.Kind() || reflect.Interface == field.Type.Kind()

    for _, rule := range splitRules(field.Tag.Get("validate")) {
        name, _ := splitRule(rule)

        if "notEmpty" == name {
            /* notEmpty on a fixed-length array is vacuous: the validator measures len(), which for [N]T (N >= 1) is always N, so it never rejects the field, present or absent — a non-pointer fixed array is therefore not required. A *[N]T stays required, since the validator rejects the nil pointer. */
            if reflect.Array == field.Type.Kind() && 1 <= field.Type.Len() {
                return false
            }

            return true
        }

        if "notBlank" == name {
            if true == absenceIsNil || ("string" == schema.Type && false == isStructuralStringFormat(schema.Format)) {
                return true
            }
        }
    }

    return false
}

/* fixedArrayNotEmptyIsVacuous reports whether a field is a non-pointer fixed-length array carrying notEmpty, for which the validator's length check can never fail — the property is neither required nor floored, so the spec matches the server that accepts any array length (a longer payload truncates, a shorter one zero-fills). */
func fixedArrayNotEmptyIsVacuous(field reflect.StructField) bool {
    if reflect.Array != field.Type.Kind() || 1 > field.Type.Len() {
        return false
    }

    for _, rule := range splitRules(field.Tag.Get("validate")) {
        name, _ := splitRule(rule)
        if "notEmpty" == name {
            return true
        }
    }

    return false
}

/* @important byte and date-time come from buildSchema and mark a non-string Go value rendered as a string, whose raw value the validator's string constraints never measure the way the spec text suggests; the email format is an annotation applyValidation itself added on a genuine string, and every later string facet still applies to it. */
func isStructuralStringFormat(format string) bool {
    return "" != format && "email" != format
}

/* parseLeadingInt must accept exactly the numeric bound strings the validator's parseIntStrict accepts (validation/validation_rule.go): the openapi mirror decides a field is unsatisfiable when a numeric bound is malformed, so if the two diverged the spec would advertise satisfiability the validator does not honour (or the reverse). Both scan with fmt.Sscanf("%d") into the platform int — the same width parseIntStrict uses — so a bound in the 2^31..2^63-1 range that overflows a 32-bit int is rejected here exactly as the validator rejects it, instead of parsing into a wider int64 and wrapping to a satisfiable facet the validator refuses. TestParseLeadingIntMatchesValidator locks the equivalence. Keep them in lockstep — change one only alongside the other and that test. */
func parseLeadingInt(valueString string) (int, bool) {
    var result int
    if _, scanErr := fmt.Sscanf(valueString, "%d", &result); nil != scanErr {
        return 0, false
    }

    return result, true
}

func applyValidation(schema *Schema, validateTag string) {
    if true == tagHasInvalidSyntax(validateTag) {
        /* @important the validator's parseValidationTag rejects a malformed tag with a value-independent "invalid validation tag syntax" error before any value is examined (createConstraintWithParams never runs), so the field accepts no value of any kind — null included. The mirror's splitRule is lenient (it silently drops a malformed parameter pair and returns an empty-param rule), so without this guard applyValidation would advertise a satisfiable schema for a field the validator rejects outright — e.g. a stray paren token such as min(5)/notEmpty(foo)/email(x). Detect the syntax error up front and advertise the field unsatisfiable, mirroring the malformed-numeric-bound and uncompilable-regex reject-all cases. */
        markFieldUnsatisfiable(schema)
        return
    }

    if true == tagHasUnconsumedParameterizedParams(validateTag) {
        /* @important a parameterizable constraint (min/max/greaterThan/lessThan/regex) that receives parameters without its recognized key ("value", or "pattern"/"value" for regex) fails closed in the validator: WithParams returns an error and createConstraintWithParams returns invalid-rule before Constraint.Validate runs, so the field accepts no value of any kind — advertise it unsatisfiable, mirroring tagHasParamsOnNonParameterizable for the parameterizable constraints. A bare value-less constraint (no parameters at all) still resolves to the registered default and is handled by the per-case branches below. */
        markFieldUnsatisfiable(schema)
        return
    }

    if "" != schema.Ref || nil != schema.AllOf {
        /* @important a $ref (or nullable allOf-wrapped $ref) denotes a struct, which the validator rejects outright for notEmpty and greaterThan/lessThan, and it fails closed on params a non-parameterizable constraint cannot consume — so advertise the field unsatisfiable rather than as a satisfiable object. No length/numeric facet otherwise attaches to a $ref. */
        if true == tagRejectsStruct(validateTag) || true == tagHasParamsOnNonParameterizable(validateTag) || true == tagRejectsAllViaNumericBound(validateTag) {
            markFieldUnsatisfiable(schema)
        } else if true == tagForbidsNullStruct(validateTag) {
            /* @important notBlank on a pointer-to-struct field (rendered as a nullable allOf-wrapped $ref): the validator rejects a nil pointer (dereferenceValue returns ok=false) but stringifies a non-nil struct via %v and accepts it, so the field is satisfiable with a non-null value — clear only the nullable advertisement so the spec does not offer a null the validator rejects, rather than marking the whole field unsatisfiable. */
            schema.Nullable = false
        }
        return
    }

    patterns := []string{}
    rejectsAll := false
    emptyValueSpace := false

    /* @important an unparseable bound or a negative max fails the rule closed before the value is examined (validator.go), rejecting every value of every kind — detect this kind-independent reject-all up front, since the per-kind cases below only cover valid string/numeric/boolean bounds. */
    if true == tagRejectsAllViaNumericBound(validateTag) {
        rejectsAll = true
    }

    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)

        switch name {
        case "email":
            if 0 != len(params) {
                /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams returns invalid-rule), so the field accepts no value — advertise it unsatisfiable rather than as a satisfiable field a client would trust */
                rejectsAll = true

                continue
            }
            /* @important only annotate a genuine string, so a structural format such as byte (for a []byte field) is preserved and the spec does not advertise an email constraint the validator cannot enforce on non-string values; the annotation is transparent to the later string facets — the validator enforces min/max/regex on the same raw string it checks for email, so the guards below must not treat it as structural */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                schema.Format = "email"
            }
        case "min":
            /* @important known limitation: a length min/max on a NON-string field is mirrored only where cheaply provable unsatisfiable — an unparseable bound and a below-minimum max on integer/number/boolean (below). A valid-but-rejecting bound on a numeric/array/map/$ref field is left advertised as satisfiable: precise mirroring would need per-kind, per-int-width stringification-length modelling not worth it for an already-misconfigured tag. The structural-format guard restricts the length facet to a genuine string: a []byte renders as a string with format byte and time.Time as a date-time string, but the validator's MinLength/MaxLength measure fmt.Sprintf("%v", value) (e.g. "[1 2 3]" for a []byte), not the base64/date-time text, so a facet here would over-constrain those fields — mirror the email branch and stay off them. */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        value := int(parsed)
                        /* @important OpenAPI requires minLength to be non-negative; a negative bound (a nonsensical tag such as min=-5) is clamped to 0 so the generated document stays spec-valid, and the validator enforces no minimum for a negative bound either. */
                        if 0 > value {
                            value = 0
                        }
                        /* @important raise-only so a degenerate min=0 cannot lower a notEmpty/notBlank floor of 1 that was applied earlier in tag order (the validator still rejects the empty value); a real min still wins because it is larger */
                        if nil == schema.MinLength || value > *schema.MinLength {
                            schema.MinLength = &value
                        }
                    } else {
                        /* @important a malformed min value (e.g. min=abc) makes the validator fail the whole field closed (parseIntStrict rejects it), so the field accepts no value — flag it unsatisfiable rather than advertise a passable default the client would trust */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less min constraint is enforced as minLength 1 by the validator, so the spec must advertise the same bound — but raise-only, matching the value-carrying branch and the value-less max branch: a higher floor already set by another rule (an earlier real min=5, a notEmpty/notBlank floor) must not be lowered back to 1, or the spec would advertise a minLength the validator rejects values below. */
                    defaultMinLength := 1
                    if nil == schema.MinLength || defaultMinLength > *schema.MinLength {
                        schema.MinLength = &defaultMinLength
                    }
                }
            }
        case "max":
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        value := int(parsed)
                        if 0 > value {
                            /* @important a negative max makes MaxLength.Validate (len > max) reject every NON-NULL value including the empty string; but the validator dereferences a nil pointer to "absent" and passes, so a nullable field still accepts null (emptyValueSpace) while a non-nullable one accepts nothing (rejectsAll) — matching the integer/number/boolean branch below rather than clearing Nullable on a value the validator admits */
                            if true == schema.Nullable {
                                emptyValueSpace = true
                            } else {
                                rejectsAll = true
                            }
                        } else if nil == schema.MaxLength || value < *schema.MaxLength {
                            /* @important tighten-only: the validator enforces every rule on the field, so when another rule already set a lower ceiling (for example an uncompilable regex sets maxLength 0, admitting only the empty string) a looser max must not raise it back and resurrect values that rule rejects; the intersection keeps the tighter bound */
                            schema.MaxLength = &value
                        }
                    } else {
                        /* @important a malformed max value makes the validator fail the whole field closed, so flag it unsatisfiable */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less max constraint is enforced as maxLength 100 by the validator, so the spec must advertise the same bound — but tighten-only, matching the value-carrying branch: a lower ceiling already set by another rule (an uncompilable regex sets maxLength 0, an earlier smaller max) must not be raised back to 100, or the spec would offer values the validator rejects */
                    defaultMaxLength := 100
                    if nil == schema.MaxLength || defaultMaxLength < *schema.MaxLength {
                        schema.MaxLength = &defaultMaxLength
                    }
                }
            } else if "integer" == schema.Type || "number" == schema.Type || "boolean" == schema.Type {
                /* @important MaxLength.Validate stringifies any value with %v (len > max), so max applies to integer/number/boolean too; the shortest stringification is 1 char for a number and 4 for a boolean ("true"). A bound below that minimum admits no non-null value: a nullable field keeps null valid (emptyValueSpace), a non-nullable one accepts nothing (rejectsAll). No exact OpenAPI facet exists for a stringified-length ceiling, so a satisfiable bound is left unconstrained. */
                minStringLength := 1
                if "boolean" == schema.Type {
                    minStringLength = 4
                }
                tooSmall := false
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        if int(parsed) < minStringLength {
                            tooSmall = true
                        }
                    } else {
                        tooSmall = true
                    }
                }
                if true == tooSmall {
                    if true == schema.Nullable {
                        emptyValueSpace = true
                    } else {
                        rejectsAll = true
                    }
                }
            }
        /* only "regex": the validator registers no constraint named "pattern" (validator.go registers ConstraintRegex == "regex"), so treating it as an alias advertised a satisfiable pattern for a field the validator rejects for every value. Like any other name this mirror does not model — a caller's custom constraint among them — it is now left out of the schema, which under-constrains the spec rather than contradicting it. */
        case "regex":
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                pattern := patternParam(params)
                if _, compileErr := regexp.Compile(pattern); nil != compileErr {
                    /* @important a pattern that fails to compile is NOT emitted verbatim (an uncompilable pattern is invalid in the spec and most OpenAPI tooling rejects it). Regex.Validate (constraint_regex.go) short-circuits and accepts the empty string and a nil pointer BEFORE the invalid-pattern branch, rejecting only non-empty values — so the faithful advertisement is maxLength 0 (only the empty string satisfies) with the nullable advertisement left intact, not a fully unsatisfiable field. */
                    zeroLength := 0
                    if nil == schema.MaxLength || 0 < *schema.MaxLength {
                        schema.MaxLength = &zeroLength
                    }
                } else {
                    patterns = append(patterns, pattern)
                }
            }
        case "alpha":
            if 0 != len(params) {
                /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams returns invalid-rule), so the field accepts no value — advertise it unsatisfiable rather than as a satisfiable field a client would trust */
                rejectsAll = true

                continue
            }
            /* @important the validator enforces these character classes with an anchored pattern but short-circuits on an empty string (it accepts ""), so advertise the class with a * quantifier — which also matches "" — rather than + which would reject the "" the validator accepts. The structural-format guard keeps the pattern off a []byte (format byte) or time.Time (date-time) field, where the validator's string constraint never sees a string value and silently accepts every blob, so a pattern would over-constrain a base64/date-time payload the server actually admits. */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                patterns = append(patterns, "^[a-zA-Z]*$")
            }
        case "numeric":
            if 0 != len(params) {
                /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams returns invalid-rule), so the field accepts no value — advertise it unsatisfiable rather than as a satisfiable field a client would trust */
                rejectsAll = true

                continue
            }
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                patterns = append(patterns, "^[0-9]*$")
            }
        case "alphanumeric":
            if 0 != len(params) {
                /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams returns invalid-rule), so the field accepts no value — advertise it unsatisfiable rather than as a satisfiable field a client would trust */
                rejectsAll = true

                continue
            }
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                patterns = append(patterns, "^[a-zA-Z0-9]*$")
            }
        case "notBlank":
            if 0 != len(params) {
                /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams returns invalid-rule), so the field accepts no value — advertise it unsatisfiable rather than as a satisfiable field a client would trust */
                rejectsAll = true

                continue
            }
            /* @important notBlank rejects a null pointer for a field of any kind (dereferenceValue returns ok=false), so the spec must never advertise the field as nullable regardless of its generated type — clear nullable unconditionally (mirroring notEmpty), not only on the string path, so a *int/*bool/*float64/*[]T/*map/*struct field is not advertised as accepting null the validator rejects. The length floor is string-only: for a string, notBlank also rejects an empty (or whitespace-only) value, so advertise minLength 1 (the OpenAPI required list only means the key is present, so an empty value would still satisfy it; a client must not send "" against the spec and then be rejected). An explicit min >= 1 in either tag order still wins, but a degenerate min=0 is raised to 1 because notBlank forbids the empty value. The whitespace-only rejection cannot be expressed by minLength. For non-string kinds notBlank accepts any non-null value (its %v stringification is non-blank), so no length/items/properties floor is advertised. */
            schema.Nullable = false
            /* the byte format is excluded from the floor: an empty []byte stringifies as "[]" (non-blank) and passes, so minLength 1 on the base64 string would reject the empty blob the validator accepts; a date-time string keeps the harmless floor since no valid rendering is empty */
            if "string" == schema.Type && "byte" != schema.Format {
                if nil == schema.MinLength || 1 > *schema.MinLength {
                    minLength := 1
                    schema.MinLength = &minLength
                }
            }
        case "notEmpty":
            if 0 != len(params) {
                /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams returns invalid-rule), so the field accepts no value — advertise it unsatisfiable rather than as a satisfiable field a client would trust */
                rejectsAll = true

                continue
            }
            /* @important notEmpty rejects a zero-length string, array, slice, or map and rejects a null pointer, so the spec must neither advertise the field as nullable nor accept an empty value. Advertise the matching length floor for whichever shape the field generated — minLength 1 for a string, minItems 1 for an array, minProperties 1 for a map (object with additionalProperties) — and clear nullable so a *string/*[]T/*map field is not advertised as accepting null; otherwise a client trusting the spec sends a null or empty value and is then rejected by the validator. An explicit min >= 1 in either tag order still wins, but a degenerate min=0 is raised to 1 because notEmpty forbids the empty value. A struct value (an inline struct object, a named-struct $ref, or any non string/array/slice/map kind) is instead advertised unsatisfiable, because the validator rejects it outright (constraint_not_empty.go default branch) rather than ignoring it */
            schema.Nullable = false
            switch schema.Type {
            case "string":
                if "date-time" == schema.Format {
                    /* @important a time.Time field is rendered as a date-time string (buildSchema), but notEmpty's validator reflects on the concrete value whose kind is Struct, not String, so it falls into the default branch and rejects every value outright (constraint_not_empty.go); advertise the field unsatisfiable like the int/number/bool scalars below rather than as a satisfiable date-time string a client would trust */
                    rejectsAll = true
                } else if nil == schema.MinLength || 1 > *schema.MinLength {
                    minLength := 1
                    schema.MinLength = &minLength
                }
            case "array":
                if nil == schema.MinItems {
                    minItems := 1
                    schema.MinItems = &minItems
                }
            case "object":
                if nil != schema.AdditionalProperties {
                    /* @important a map renders as an object with additionalProperties; the validator accepts a non-empty map (its kind is Map), so advertise the matching floor minProperties 1 */
                    if nil == schema.MinProperties {
                        minProperties := 1
                        schema.MinProperties = &minProperties
                    }
                } else {
                    /* @important an inline (anonymous) struct renders as an object with no additionalProperties; the validator reflects on the concrete value whose kind is Struct and rejects it outright (constraint_not_empty.go default branch), so advertise the field unsatisfiable rather than as a satisfiable object whose fixed property set a client would trust */
                    rejectsAll = true
                }
            case "":
                /* an interface field carries no type and the validator judges the DECODED dynamic value — a non-empty string or map passes notEmpty — so the field stays satisfiable and unconstrained */
            default:
                /* @important notEmpty's validator rejects any value whose kind is not string/array/slice/map outright (constraint_not_empty.go default branch), so an integer/number/boolean field carrying notEmpty is unsatisfiable server-side; advertise it as such — an empty exclusive number range or, for a boolean, two contradictory enums under allOf — instead of an unconstrained scalar a client would trust */
                rejectsAll = true
            }
        case "greaterThan":
            if "integer" == schema.Type || "number" == schema.Type {
                /* @important the validator rejects a null pointer for greaterThan/lessThan, so the spec must not advertise the field as nullable */
                schema.Nullable = false
                exclusive := true
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        /* @important the validator enforces every duplicate rule, so the effective floor is the highest bound: raise-only, mirroring the min/max length branches */
                        value := float64(parsed)
                        if nil == schema.Minimum || value > *schema.Minimum {
                            schema.Minimum = &value
                            schema.ExclusiveMinimum = &exclusive
                        }
                    } else {
                        /* @important a malformed greaterThan value makes the validator fail the whole field closed, so flag it unsatisfiable rather than advertise the > 0 default */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less greaterThan constraint is enforced as > 0 by the validator, so the spec must advertise the same bound */
                    value := float64(0)
                    if nil == schema.Minimum || value > *schema.Minimum {
                        schema.Minimum = &value
                        schema.ExclusiveMinimum = &exclusive
                    }
                }
            } else if "" != schema.Type {
                /* @important greaterThan's validator rejects a non-numeric value outright ("value must be numeric", constraint_greater_than.go default branch), so a string/boolean/array/object field carrying greaterThan is unsatisfiable server-side; advertise it as such rather than as an unconstrained value a client would trust. An interface field (no type) is exempt: the validator judges the DECODED dynamic value, so a numeric payload passes and the field stays satisfiable. */
                rejectsAll = true
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {
                /* @important the validator rejects a null pointer for greaterThan/lessThan, so the spec must not advertise the field as nullable */
                schema.Nullable = false
                exclusive := true
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        /* @important the validator enforces every duplicate rule, so the effective ceiling is the lowest bound: tighten-only, mirroring the min/max length branches */
                        value := float64(parsed)
                        if nil == schema.Maximum || value < *schema.Maximum {
                            schema.Maximum = &value
                            schema.ExclusiveMaximum = &exclusive
                        }
                    } else {
                        /* @important a malformed lessThan value makes the validator fail the whole field closed, so flag it unsatisfiable rather than advertise the < 0 default */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less lessThan constraint is enforced as < 0 by the validator, so the spec must advertise the same bound */
                    value := float64(0)
                    if nil == schema.Maximum || value < *schema.Maximum {
                        schema.Maximum = &value
                        schema.ExclusiveMaximum = &exclusive
                    }
                }
            } else if "" != schema.Type {
                /* @important lessThan's validator rejects a non-numeric value outright ("value must be numeric", constraint_less_than.go default branch), so a string/boolean/array/object field carrying lessThan is unsatisfiable server-side; advertise it as such rather than as an unconstrained value a client would trust. An interface field (no type) is exempt: the validator judges the DECODED dynamic value, so a numeric payload passes and the field stays satisfiable. */
                rejectsAll = true
            }
        }
    }

    if 1 == len(patterns) {
        schema.Pattern = patterns[0]
    } else if 1 < len(patterns) {
        /* @important the validator enforces every pattern rule on the field, but a single OpenAPI `pattern` holds only one (RE2 has no lookahead to AND them), so emit each as an allOf member the client must satisfy together instead of silently dropping all but the last */
        for _, pattern := range patterns {
            schema.AllOf = append(schema.AllOf, &Schema{Pattern: pattern})
        }
    }

    if true == rejectsAll {
        markFieldUnsatisfiable(schema)
    } else if true == emptyValueSpace {
        /* @important the field's non-null value space is empty (e.g. a nullable scalar with a max bound below the shortest possible stringification) but the validator still accepts null, so contradict only the value while preserving the nullable advertisement */
        applyEmptyValueSpace(schema)
    }
}

/* @important markFieldUnsatisfiable advertises a schema no value — null included — can satisfy, mirroring a validator that rejects the field outright for a malformed numeric/length tag, a negative max (both fail the field closed), or a constraint applied to a kind the validator rejects (notEmpty on a struct, greaterThan/lessThan on a non-numeric). It clears Nullable (the validator rejects null too, so an unsatisfiable field must not advertise null as valid) and then contradicts the value space via applyEmptyValueSpace. */
func markFieldUnsatisfiable(schema *Schema) {
    schema.Nullable = false
    applyEmptyValueSpace(schema)
}

/* @important applyEmptyValueSpace advertises a non-null value space no value can satisfy (the impossible facets of markFieldUnsatisfiable) WITHOUT touching Nullable, for a validator that rejects every non-null value yet still accepts null — e.g. a nullable scalar carrying a `max` bound below its shortest stringification, where MaxLength.Validate passes a nil pointer. markFieldUnsatisfiable layers a Nullable clear on top for the stricter constraints (notEmpty/greaterThan/lessThan, malformed/negative tags) whose validator rejects null as well. A string gets an impossible length window (minLength 1, maxLength 0); a number an empty exclusive range (greater than 0 and less than 0); an array minItems 1 with maxItems 0; an object minProperties 1 with maxProperties 0; a $ref the same impossible object constraint under allOf so the documented shape is preserved. */
func applyEmptyValueSpace(schema *Schema) {
    switch schema.Type {
    case "string":
        minLength := 1
        maxLength := 0
        schema.MinLength = &minLength
        schema.MaxLength = &maxLength
    case "integer", "number":
        zero := float64(0)
        exclusive := true
        schema.Minimum = &zero
        schema.Maximum = &zero
        schema.ExclusiveMinimum = &exclusive
        schema.ExclusiveMaximum = &exclusive
    case "boolean":
        /* @important a boolean carries no numeric or length facet to contradict, and an empty enum is invalid under the OpenAPI 3.0 meta-schema (enum requires minItems 1), so advertise two contradictory single-value enums under allOf: a value cannot be both true and false, yet each enum is non-empty and spec-valid */
        schema.AllOf = []*Schema{
            {Enum: &[]any{true}},
            {Enum: &[]any{false}},
        }
    case "array":
        minItems := 1
        maxItems := 0
        schema.MinItems = &minItems
        schema.MaxItems = &maxItems
    case "object":
        /* @important a map (object with additionalProperties) or an inline struct (object with a fixed property set) is contradicted at the object level: a value cannot have both at least one and at most zero properties */
        minProperties := 1
        maxProperties := 0
        schema.MinProperties = &minProperties
        schema.MaxProperties = &maxProperties
    case "":
        /* @important a $ref (or a nullable allOf-wrapped $ref) carries no Type; it denotes a struct component, so contradict it at the object level under allOf — the documented $ref is preserved as a member, but the impossible minProperties/maxProperties sibling means no value satisfies the conjunction */
        minProperties := 1
        maxProperties := 0
        contradiction := &Schema{MinProperties: &minProperties, MaxProperties: &maxProperties}
        if "" != schema.Ref {
            schema.AllOf = []*Schema{{Ref: schema.Ref}, contradiction}
            schema.Ref = ""
        } else if nil != schema.AllOf {
            schema.AllOf = append(schema.AllOf, contradiction)
        } else {
            /* @important an interface field carries neither a type nor a $ref, so the object-level contradiction would not bind — the empty schema admits every value; not against the empty schema is the one advertisement no value satisfies */
            schema.Not = &Schema{}
        }
    }
}

/* @important reports whether a validate tag is malformed the way the runtime validator's parseValidationTag treats as a hard error: it returns "invalid validation tag syntax" — before any value is examined — for a parenthesized part that does not close with ')', has an empty constraint name, carries unbalanced parameter brackets, or contains a parameter pair without '=' or with an empty key; and for a '='-bearing part with an empty name. The mirror's splitRule tolerates all of these (it skips a malformed pair and returns an empty-param rule), so applyValidation would otherwise emit a satisfiable schema for a field the validator rejects for every value. This mirrors parseValidationTag exactly, reusing the same top-level (splitRules), parameter (splitRuleParameters) and bracket-balance (hasBalancedRuleBrackets) split helpers, so a valid tag never trips it. */
func tagHasInvalidSyntax(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        part := strings.TrimSpace(rule)
        if "" == part {
            continue
        }

        openIndex := strings.IndexByte(part, '(')
        equalIndex := strings.IndexByte(part, '=')

        isParenthesized := -1 != openIndex && (-1 == equalIndex || openIndex < equalIndex)

        if true == isParenthesized {
            if false == strings.HasSuffix(part, ")") {
                return true
            }

            name := strings.TrimSpace(part[:openIndex])
            if "" == name {
                return true
            }

            paramsString := strings.TrimSpace(part[openIndex+1 : len(part)-1])
            if false == hasBalancedRuleBrackets(paramsString) {
                return true
            }

            if "" != paramsString {
                for _, pair := range splitRuleParameters(paramsString) {
                    pair = strings.TrimSpace(pair)
                    if "" == pair {
                        continue
                    }

                    separator := strings.IndexByte(pair, '=')
                    if -1 == separator {
                        return true
                    }

                    if "" == strings.TrimSpace(pair[:separator]) {
                        return true
                    }
                }
            }

            continue
        }

        if true == strings.Contains(part, "=") {
            if "" == strings.TrimSpace(part[:equalIndex]) {
                return true
            }
        }
    }

    return false
}

/* @important reports whether a validate tag carries parameters on a constraint that cannot consume them: the validator fails such a rule closed (createConstraintWithParams returns invalid-rule before Constraint.Validate runs), so the field accepts no value of any kind — including a struct behind a $ref/allOf, which the in-switch guards below never see because applyValidation returns early for those schemas. */
func tagHasParamsOnNonParameterizable(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
        if 0 == len(params) {
            continue
        }

        switch name {
        case "email", "alpha", "numeric", "alphanumeric", "notBlank", "notEmpty":
            return true
        }
    }

    return false
}

/* @important reports whether a validate tag carries parameters a PARAMETERIZABLE constraint cannot consume: min/max/greaterThan/lessThan read only the "value" key and regex only "pattern"/"value", so a non-empty parameter set lacking the recognized key makes WithParams fail the rule closed (createConstraintWithParams returns invalid-rule before Constraint.Validate runs), and the field accepts no value of any kind. Mirrors tagHasParamsOnNonParameterizable for the parameterizable constraints. A bare constraint with no parameters is excluded (0 == len(params)) because it resolves to the registered default rather than failing closed. */
func tagHasUnconsumedParameterizedParams(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
        if 0 == len(params) {
            continue
        }

        switch name {
        case "min", "max", "greaterThan", "lessThan":
            if _, exists := params["value"]; false == exists {
                return true
            }
        case "regex":
            _, hasPattern := params["pattern"]
            _, hasValue := params["value"]
            if false == hasPattern && false == hasValue {
                return true
            }
        }
    }

    return false
}

/* @important reports whether a validate tag carries a constraint the runtime validator rejects outright for a struct value: notEmpty falls into constraint_not_empty.go's default branch, and greaterThan/lessThan into their "value must be numeric" default branch. A $ref/allOf schema (always a struct component) carrying any of these is therefore unsatisfiable server-side. notBlank is excluded because it stringifies any value with %v and only rejects a blank or nil one, so it does not reject a struct outright. */
func tagRejectsStruct(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, _ := splitRule(rule)
        switch name {
        case "notEmpty", "greaterThan", "lessThan":
            return true
        }
    }

    return false
}

/* @important reports whether a min/max/greaterThan/lessThan tag carries a value the validator's parseIntStrict cannot parse (malformed or empty): createConstraintWithParams then fails the rule — GreaterThan/LessThan.WithParams run the same parse — and validateRule returns the error BEFORE the field value is examined, so the validator rejects every value of every kind — a nil pointer included — making the field unconditionally unsatisfiable (null too). This is the only kind-independent reject-all case: a VALID but restrictive bound (a negative max, a too-large min) still lets MaxLength/MinLength.Validate run, and those accept a nil pointer (dereferenceValue returns ok=false), so a nullable field keeps null valid and is handled by the per-kind emptyValueSpace branches, not here. */
func tagRejectsAllViaNumericBound(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
        if "min" != name && "max" != name && "greaterThan" != name && "lessThan" != name {
            continue
        }

        valueString, exists := params["value"]
        if false == exists {
            continue
        }

        if _, parsedOk := parseLeadingInt(valueString); false == parsedOk {
            return true
        }
    }

    return false
}

/* @important reports whether a validate tag forbids a null value while still accepting a non-null struct — only notBlank qualifies: it rejects a nil pointer (dereferenceValue returns ok=false) yet stringifies a non-nil struct with %v and accepts it, so a pointer-to-struct field carrying notBlank is satisfiable but must not advertise null. notEmpty/greaterThan/lessThan also reject null but additionally reject the struct outright, so they are handled by tagRejectsStruct/markFieldUnsatisfiable rather than here. */
func tagForbidsNullStruct(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, _ := splitRule(rule)
        if "notBlank" == name {
            return true
        }
    }

    return false
}

func patternParam(params map[string]string) string {
    if pattern, exists := params["pattern"]; true == exists {
        return pattern
    }

    return params["value"]
}

func splitRules(validateTag string) []string {
    trimmed := strings.TrimSpace(validateTag)
    if "" == trimmed || "-" == trimmed {
        return nil
    }

    return splitTopLevelRules(trimmed)
}

/* @important tracks whether the scan is inside a regex character class [...] so the bracket/comma bookkeeping treats ')', ']', '}', '(', '{' and ',' as literal class members. A ']' is a literal (not a close) when it is the class's first content character — and the leading negation '^' does not count as content — mirroring regexp/syntax. A POSIX named element ([:alpha:], [.coll.], [=equiv=]) nested inside the class is tracked separately, because its closing ']' (which follows the ':', '.' or '=' delimiter) must not be mistaken for the ']' that closes the whole class — otherwise a valid tag such as regex=[[:alpha:],] would be split at the in-class comma. */
type charClassScanner struct {
    inClass            bool
    contentSeen        bool
    caretAllowed       bool
    posixOpenPending   bool
    inPosixElement     bool
    posixDelimiter     rune
    posixDelimiterSeen bool
}

func (instance *charClassScanner) step(character rune) bool {
    if true == instance.inClass {
        if true == instance.inPosixElement {
            if (']' == character) && (true == instance.posixDelimiterSeen) {
                instance.inPosixElement = false
                instance.posixDelimiter = 0
                instance.posixDelimiterSeen = false

                return true
            }

            instance.posixDelimiterSeen = instance.posixDelimiter == character

            return true
        }

        if true == instance.posixOpenPending {
            instance.posixOpenPending = false
            /* only [: :] is a POSIX class RE2 recognizes; [. .] collating and [= =] equivalence elements are NOT supported by RE2 (which treats '[' inside a class as a literal), so treating them as class openers over-extends the class and diverges from the validator-side scanner and from regexp.Compile */
            if ':' == character {
                instance.inPosixElement = true
                instance.posixDelimiter = character
                instance.posixDelimiterSeen = false

                return true
            }
        }

        if ('^' == character) && (false == instance.contentSeen) && (true == instance.caretAllowed) {
            instance.caretAllowed = false

            return true
        }

        instance.caretAllowed = false

        if '[' == character {
            instance.contentSeen = true
            instance.posixOpenPending = true

            return true
        }

        if (']' == character) && (true == instance.contentSeen) {
            instance.inClass = false

            return true
        }

        instance.contentSeen = true

        return true
    }

    if '[' == character {
        instance.inClass = true
        instance.contentSeen = false
        instance.caretAllowed = true
        instance.posixOpenPending = false
        instance.inPosixElement = false
        instance.posixDelimiter = 0
        instance.posixDelimiterSeen = false

        return true
    }

    return false
}

func (instance *charClassScanner) noteEscaped() {
    if true == instance.inClass {
        instance.caretAllowed = false
        instance.contentSeen = true
        instance.posixOpenPending = false
        instance.posixDelimiterSeen = false
    }
}

func splitTopLevelRules(input string) []string {
    var parts []string

    bracketsBalanced := hasBalancedRuleBrackets(input)

    current := strings.Builder{}
    parenDepth := 0
    curlyDepth := 0
    wasEscaped := false
    classScanner := charClassScanner{}

    for _, character := range input {
        if true == wasEscaped {
            current.WriteRune(character)
            wasEscaped = false
            classScanner.noteEscaped()
            continue
        }

        if '\\' == character {
            current.WriteRune(character)
            wasEscaped = true
            continue
        }

        if true == bracketsBalanced {
            if true == classScanner.step(character) {
                current.WriteRune(character)
                continue
            }

            if '(' == character {
                parenDepth++
                current.WriteRune(character)
                continue
            }

            if ')' == character {
                if 0 < parenDepth {
                    parenDepth--
                }
                current.WriteRune(character)
                continue
            }

            if '{' == character {
                curlyDepth++
                current.WriteRune(character)
                continue
            }

            if '}' == character {
                if 0 < curlyDepth {
                    curlyDepth--
                }
                current.WriteRune(character)
                continue
            }
        }

        if ',' == character {
            if 0 == parenDepth && 0 == curlyDepth {
                parts = append(parts, current.String())
                current.Reset()
                continue
            }
        }

        current.WriteRune(character)
    }

    parts = append(parts, current.String())

    return parts
}

func splitRuleParameters(input string) []string {
    var parts []string

    current := strings.Builder{}
    parenDepth := 0
    curlyDepth := 0
    isInSingleQuote := false
    isInDoubleQuote := false
    wasEscaped := false
    classScanner := charClassScanner{}

    for _, character := range input {
        if true == wasEscaped {
            current.WriteRune(character)
            wasEscaped = false
            classScanner.noteEscaped()
            continue
        }

        if '\\' == character {
            current.WriteRune(character)
            wasEscaped = true
            continue
        }

        if '"' == character && false == classScanner.inClass {
            if false == isInSingleQuote {
                isInDoubleQuote = false == isInDoubleQuote
            }
            current.WriteRune(character)
            continue
        }

        if '\'' == character && false == classScanner.inClass {
            if false == isInDoubleQuote {
                isInSingleQuote = false == isInSingleQuote
            }
            current.WriteRune(character)
            continue
        }

        if false == isInSingleQuote && false == isInDoubleQuote {
            if true == classScanner.step(character) {
                current.WriteRune(character)
                continue
            }

            if '(' == character {
                parenDepth++
                current.WriteRune(character)
                continue
            }

            if ')' == character {
                if 0 < parenDepth {
                    parenDepth--
                }
                current.WriteRune(character)
                continue
            }

            if '{' == character {
                curlyDepth++
                current.WriteRune(character)
                continue
            }

            if '}' == character {
                if 0 < curlyDepth {
                    curlyDepth--
                }
                current.WriteRune(character)
                continue
            }

            if ',' == character {
                if 0 == parenDepth && 0 == curlyDepth {
                    parts = append(parts, current.String())
                    current.Reset()
                    continue
                }
            }
        }

        current.WriteRune(character)
    }

    parts = append(parts, current.String())

    return parts
}

func hasBalancedRuleBrackets(input string) bool {
    parenDepth := 0
    curlyDepth := 0
    wasEscaped := false
    classScanner := charClassScanner{}

    for _, character := range input {
        if true == wasEscaped {
            wasEscaped = false
            classScanner.noteEscaped()
            continue
        }

        if '\\' == character {
            wasEscaped = true
            continue
        }

        if true == classScanner.step(character) {
            continue
        }

        /* a ']' that reaches here closes no class: RE2 reads it as a literal, so it must not sink the whole tag (the class scanner above consumes the ones that do close a class). The validator's hasBalancedBrackets says the same, and this helper is its mirror. */
        switch character {
        case '(':
            parenDepth++
        case ')':
            if 0 == parenDepth {
                return false
            }
            parenDepth--
        case '{':
            curlyDepth++
        case '}':
            if 0 == curlyDepth {
                return false
            }
            curlyDepth--
        }
    }

    return 0 == parenDepth && 0 == curlyDepth && false == classScanner.inClass
}

func splitRule(rule string) (string, map[string]string) {
    trimmed := strings.TrimSpace(rule)
    params := make(map[string]string)

    openIndex := strings.IndexByte(trimmed, '(')
    equalIndex := strings.IndexByte(trimmed, '=')
    if -1 != openIndex && true == strings.HasSuffix(trimmed, ")") && (-1 == equalIndex || openIndex < equalIndex) {
        name := strings.TrimSpace(trimmed[:openIndex])
        inner := trimmed[openIndex+1 : len(trimmed)-1]
        for _, pair := range splitRuleParameters(inner) {
            pair = strings.TrimSpace(pair)
            if "" == pair {
                continue
            }

            separator := strings.IndexByte(pair, '=')
            if -1 == separator {
                continue
            }

            key := strings.TrimSpace(pair[:separator])
            if "" == key {
                continue
            }

            params[key] = strings.TrimSpace(pair[separator+1:])
        }

        return name, params
    }

    if -1 == equalIndex {
        return trimmed, params
    }

    params["value"] = strings.TrimSpace(trimmed[equalIndex+1:])

    return strings.TrimSpace(trimmed[:equalIndex]), params
}
