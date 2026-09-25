package openapi

import (
    "encoding/json"
    "reflect"
    "regexp"
    "strconv"
    "strings"
    "sync"
    "time"
)

var timeType = reflect.TypeOf(time.Time{})
var jsonMarshalerInterfaceType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

/* maximumSchemaDepth bounds the descent into a type. Generate runs inside the spec handler and a stack overflow is fatal and cannot be recovered, so reaching the bound degrades to an undescribed position instead. */
const maximumSchemaDepth = 64

/* schemaDepthLimitDescription marks a position where the bound stopped the generator; the schema there accepts any value, so the document stays well-formed and permissive. */
var schemaDepthLimitDescription = "schema generation stopped at the maximum nesting depth of " +
    strconv.Itoa(maximumSchemaDepth) +
    " levels; this position is left undescribed"

func depthLimitSchema() *Schema {
    return &Schema{Description: schemaDepthLimitDescription}
}

/* a bare top-level unsigned type gets the zero floor here, as fields and collection elements do */
func schemaFromType(targetType reflect.Type, components map[string]*Schema, names map[reflect.Type]string) *Schema {
    schema := buildSchema(targetType, components, names, make(map[reflect.Type]bool))

    applyUnsignedLowerBound(schema, targetType)

    return schema
}

func buildSchema(targetType reflect.Type, components map[string]*Schema, names map[reflect.Type]string, visited map[reflect.Type]bool) *Schema {
    return buildSchemaAtDepth(targetType, components, names, visited, 0)
}

func buildSchemaAtDepth(
    targetType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    depth int,
) *Schema {
    if nil == targetType {
        return &Schema{}
    }

    if maximumSchemaDepth <= depth {
        return depthLimitSchema()
    }

    nullable := false
    pointerHops := 0
    for reflect.Ptr == targetType.Kind() {
        nullable = true
        targetType = targetType.Elem()

        /* a named pointer type can dereference to itself, so the hops are counted against the same bound */
        pointerHops++
        if maximumSchemaDepth <= pointerHops {
            return withNullable(depthLimitSchema(), true)
        }
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

        return withNullable(collectionSchemaReference(targetType, components, names, visited, depth), nullable)
    case reflect.Map:
        return withNullable(collectionSchemaReference(targetType, components, names, visited, depth), nullable)
    case reflect.Struct:
        if true == promotesEmbeddedTimeCodec(targetType) {
            return withNullable(&Schema{Type: "string", Format: "date-time"}, nullable)
        }

        return structSchemaReference(targetType, components, names, visited, nullable, depth)
    default:
        return withNullable(&Schema{}, nullable)
    }
}

/* elementSchema stamps the unsigned floor on a collection element, which carries no validate tag of its own and so no validator bound to weaken. */
func elementSchema(
    elementType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    depth int,
) *Schema {
    schema := buildSchemaAtDepth(elementType, components, names, visited, depth)

    applyUnsignedLowerBound(schema, elementType)

    return schema
}

/* collectionSchemaReference sends a named collection whose elements lead back to itself through components and a $ref, which ends the walk; every other collection stays inline. */
func collectionSchemaReference(
    targetType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    depth int,
) *Schema {
    if false == collectionReachesItself(targetType) {
        return collectionSchemaBody(targetType, components, names, visited, depth)
    }

    name := schemaComponentName(targetType, names)
    if _, built := components[name]; false == built {
        /* the key is claimed before descending, so an element arriving back here answers with the $ref */
        components[name] = &Schema{Type: "object"}
        components[name] = collectionSchemaBody(targetType, components, names, visited, depth)
    }

    return &Schema{Ref: "#/components/schemas/" + name}
}

func collectionSchemaBody(
    targetType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    depth int,
) *Schema {
    if reflect.Map == targetType.Kind() {
        return &Schema{Type: "object", AdditionalProperties: elementSchema(targetType.Elem(), components, names, visited, depth+1)}
    }

    return &Schema{Type: "array", Items: elementSchema(targetType.Elem(), components, names, visited, depth+1)}
}

/* collectionReachesItself follows the single element chain of a named collection and reports whether it returns to it. The walk is bounded, since a chain can fall into a cycle that never returns to its start. */
func collectionReachesItself(targetType reflect.Type) bool {
    if "" == targetType.Name() {
        return false
    }

    current := targetType
    for hop := 0; hop < maximumSchemaDepth; hop++ {
        switch current.Kind() {
        case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Map:
            current = current.Elem()
        default:
            return false
        }

        if current == targetType {
            return true
        }
    }

    return false
}

func structSchemaReference(
    structType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    nullable bool,
    depth int,
) *Schema {
    if "" == structType.Name() {
        return withNullable(buildStructSchema(structType, components, names, visited, depth), nullable)
    }

    name := schemaComponentName(structType, names)
    if _, built := components[name]; false == built {
        components[name] = &Schema{Type: "object"}
        components[name] = buildStructSchema(structType, components, names, visited, depth)
    }

    return withNullable(&Schema{Ref: "#/components/schemas/" + name}, nullable)
}

func schemaComponentName(structType reflect.Type, names map[reflect.Type]string) string {
    if existing, assigned := names[structType]; true == assigned {
        return existing
    }

    base := sanitizeComponentName(structType.Name())
    candidate := base
    suffix := 2
    for true == componentNameInUse(candidate, names) {
        candidate = base + strconv.Itoa(suffix)
        suffix++
    }

    names[structType] = candidate

    return candidate
}

var componentNamePackagePathPattern = regexp.MustCompile(`[A-Za-z0-9_.\-]*/`)
var componentNameIllegalPattern = regexp.MustCompile(`[^A-Za-z0-9._\-]+`)

/* sanitizeComponentName drops the import paths from a generic's type name and folds the remaining punctuation to one underscore: the component key grammar and the $ref pointer admit neither, and the key stays stable across runs. */
func sanitizeComponentName(name string) string {
    sanitized := componentNamePackagePathPattern.ReplaceAllString(name, "")
    sanitized = componentNameIllegalPattern.ReplaceAllString(sanitized, "_")
    sanitized = strings.Trim(sanitized, "_")

    if "" == sanitized {
        return "Schema"
    }

    return sanitized
}

func componentNameInUse(candidate string, names map[reflect.Type]string) bool {
    for _, assigned := range names {
        if assigned == candidate {
            return true
        }
    }

    return false
}

func buildStructSchema(
    structType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    depth int,
) *Schema {
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
    collectStructFields(structType, components, names, visited, schema.Properties, &required, &rejectsAll, depth)

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

/* embedTagRejectsAll reports whether a promoted embed's validate tag, which the validator runs against the embed value, rejects every payload, so the enclosing object is advertised unsatisfiable. A pointer embed under a nil-skipping constraint is over-approximated on purpose, fail-closed: the spec refuses more than the server, never less. */
func embedTagRejectsAll(field reflect.StructField) bool {
    validateTag := field.Tag.Get("validate")

    /* a bare parameterized constraint needs no check here: on a struct embed the shape refusals below already catch every one */
    return true == tagHasInvalidSyntax(validateTag) ||
        true == tagHasUnconsumedParameterizedParams(validateTag) ||
        true == tagHasParamsOnNonParameterizable(validateTag) ||
        true == tagHasEmptyRegexPattern(validateTag) ||
        true == tagRejectsReferencedValue(validateTag, "") ||
        true == tagRefusesNonStringValue(validateTag) ||
        true == tagRejectsAllViaNumericBound(validateTag)
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
    depth int,
) {
    resolved := make(map[string]bool)

    embeddedSeen := make(map[reflect.Type]bool)
    embeddedSeen[structType] = true

    /* embedCount tracks how many equal-depth paths reach an embedded type, mirroring encoding/json: a type reached via N paths has its fields duplicated N times so a diamond annihilates in dominantEmbeddedField. */
    embedCount := make(map[reflect.Type]int)

    ownCandidatesByName := make(map[string][]embeddedCandidate)
    var ownOrder []string
    var embedQueue []reflect.Type
    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if true == isPromotedEmbed(field) {
            /* the validator cannot read an unexported embed through Interface and leaves its tag unenforced, so the mirror does too */
            if true == field.IsExported() && true == embedTagRejectsAll(field) {
                *rejectsAll = true
            }

            embeddedType := dereferencedType(field.Type)
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

        addFieldProperty(winner, jsonName, components, names, visited, properties, required, depth)
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

                    childType := dereferencedType(field.Type)
                    if true == embeddedSeen[childType] {
                        continue
                    }
                    if 0 == nextCount[childType] {
                        nextLevel = append(nextLevel, childType)
                    }
                    /* two copies decide every dominance tie, so the count is capped there; it counts this level's occurrences, as encoding/json's typeFields does */
                    nextCount[childType] += 1
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

            addFieldProperty(winner, jsonName, components, names, visited, properties, required, depth)
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
    depth int,
) {
    propertySchema := buildSchemaAtDepth(field.Type, components, names, visited, depth+1)
    tagRejectsAllValues := applyValidation(propertySchema, field.Tag.Get("validate"), components)

    /* a fixed-length array never falls below notEmpty's floor, so the floor is dropped, unless another rule's contradiction still needs it */
    if true == fixedArrayNotEmptyIsVacuous(field) && nil == propertySchema.MaxItems {
        propertySchema.MinItems = nil
    }

    /* a referenced component's floor is stamped beside the schema and moved onto the reference after the fixed-array corrections and before the contradiction, which stays last */
    liftValidationFacetsOntoReference(propertySchema)

    /* a zero-length fixed array under notEmpty rejects every payload */
    if true == fixedArrayNotEmptyIsUnsatisfiable(field) {
        markFieldUnsatisfiable(propertySchema)
    }

    /* the required and validation decisions read the bare scalar schema, since the validator sees the decoded value; the quoted form only changes the spelling */
    /* a rule set that rejects every value rejects the absent field too, since it decodes to the zero value, so the field is listed required; so is a non-pointer field of an unsatisfiable component, while a pointer field stays optional */
    fieldRequired := true == isRequired(field, propertySchema) ||
        true == pointerBoundRequiresPresence(field) ||
        true == zeroValueRejectsAbsentProperty(field, propertySchema) ||
        true == tagRejectsAllValues ||
        (reflect.Ptr != field.Type.Kind() && true == referencedComponentRejectsAll(propertySchema, components))

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

/* applyUnsignedLowerBound stamps a zero floor on an unsigned field, raise-only, so it never weakens a bound the validator placed at or above zero. */
func applyUnsignedLowerBound(schema *Schema, fieldType reflect.Type) {
    if nil == fieldType {
        return
    }

    fieldType = dereferencedType(fieldType)

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

/* jsonStringOptionKind reports the kind a ",string" option applies to: encoding/json honours it only on integer, float and boolean fields, advertised then as a string. A string field's double-encoded form has no faithful schema and is left alone. */
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

    /* encoding/json dereferences exactly one unnamed pointer before applying the option */
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

/* quotedScalarSchema advertises the textual form the ",string" decoder accepts: base-10 digits for an integer, the two literals for a boolean, and no pattern for a float, whose grammar is wider than JSON's. The quoted "null" is advertised only where the validator accepts its decoded outcome, and an unsatisfiable scalar stays unsatisfiable. */
func quotedScalarSchema(original *Schema, valueKind reflect.Kind, isPointer bool) *Schema {
    quoted := &Schema{Type: "string", Nullable: original.Nullable}

    if true == scalarSchemaRejectsAll(original) {
        /* a nullable empty value space accepts exactly the null literal */
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

/* quotedNullAccepted follows what the validator accepts after a quoted "null": on a pointer target the Nullable flag, on a bare scalar whether the window still admits zero. */
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

/* recognizes the shapes applyEmptyValueSpace stamps on a scalar (the empty exclusive number window, the contradictory boolean enums) plus a genuinely empty greaterThan/lessThan window — for an integer including the adjacent-bounds window no whole number fits, such as the open interval (5, 6). */
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

/* zeroValueRejectsAbsentProperty reports whether the zero value an omitted property decodes to fails the field's constraints, which the validator checks on every tagged field; such a field is listed required. */
func zeroValueRejectsAbsentProperty(field reflect.StructField, schema *Schema) bool {
    isPointer := reflect.Ptr == field.Type.Kind()

    for _, rule := range parsedTagRules(field.Tag.Get("validate")) {
        name, params := rule.name, rule.params

        switch name {
        case "min":
            /* an omitted pointer stays nil, which the length constraint accepts, so the field stays optional */
            if true == isPointer || "string" != schema.Type || true == isStructuralStringFormat(schema.Format) {
                continue
            }
            valueString, exists := params["value"]
            if false == exists {
                continue
            }
            bound, parsedOk := parseBoundStrict(valueString)
            if false == parsedOk {
                continue
            }
            if bound >= 1 {
                return true
            }
        case "greaterThan":
            if true == isPointer || ("integer" != schema.Type && "number" != schema.Type) {
                continue
            }
            valueString, exists := params["value"]
            if false == exists {
                continue
            }
            bound, parsedOk := parseBoundStrict(valueString)
            if false == parsedOk {
                continue
            }
            if bound >= 0 {
                return true
            }
        case "lessThan":
            if true == isPointer || ("integer" != schema.Type && "number" != schema.Type) {
                continue
            }
            valueString, exists := params["value"]
            if false == exists {
                continue
            }
            bound, parsedOk := parseBoundStrict(valueString)
            if false == parsedOk {
                continue
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

    for _, rule := range parsedTagRules(field.Tag.Get("validate")) {
        name := rule.name
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

/* dereferencedType strips pointers under a bound, since a named pointer type can dereference to itself; a chain that outlasts the bound reads as not the kind asked about. */
func dereferencedType(targetType reflect.Type) reflect.Type {
    for hop := 0; hop < maximumSchemaDepth; hop++ {
        if reflect.Ptr != targetType.Kind() {
            return targetType
        }

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

    embedded := dereferencedType(field.Type)

    return reflect.Struct == embedded.Kind()
}

/* promotesEmbeddedTimeCodec reports whether time.Time is the type an embedded MarshalJSON is promoted from, so the value is written as an RFC 3339 string. The validator's promotesValidationTimeCodec also asks the decode side, so the two differ on purpose and share promotedMarshalerOrigin. */
func promotesEmbeddedTimeCodec(structType reflect.Type) bool {
    if false == structType.Implements(jsonMarshalerInterfaceType) {
        return false
    }

    origin, depth, resolved := promotedMarshalerOrigin(structType, make(map[reflect.Type]bool))
    if false == resolved {
        return false
    }

    return 0 < depth && timeType == origin
}

/* promotedMarshalerOrigin reports which type declares the MarshalJSON in the value method set and at what depth: the shallowest wins and a tie promotes nothing. It answers false where reflection cannot settle the question, and the caller keeps the object schema. */
func promotedMarshalerOrigin(targetType reflect.Type, path map[reflect.Type]bool) (reflect.Type, int, bool) {
    if timeType == targetType {
        return timeType, 0, true
    }

    if reflect.Struct != targetType.Kind() {
        return targetType, 0, true
    }

    if true == path[targetType] {
        return nil, 0, false
    }
    path[targetType] = true
    defer delete(path, targetType)

    var winner reflect.Type
    winnerDepth := 0
    winnerCount := 0

    for index := 0; index < targetType.NumField(); index++ {
        field := targetType.Field(index)
        if false == field.Anonymous {
            continue
        }

        if false == field.Type.Implements(jsonMarshalerInterfaceType) {
            continue
        }

        embedded := field.Type
        if reflect.Ptr == embedded.Kind() {
            embedded = dereferencedType(embedded)

            /* a pointer-receiver codec cannot be followed through the embed, so the origin is unresolved */
            if false == embedded.Implements(jsonMarshalerInterfaceType) {
                return nil, 0, false
            }
        }

        origin, originDepth, resolved := promotedMarshalerOrigin(embedded, path)
        if false == resolved {
            return nil, 0, false
        }

        originDepth++

        if 0 == winnerCount || originDepth < winnerDepth {
            winner = origin
            winnerDepth = originDepth
            winnerCount = 1

            continue
        }

        if originDepth == winnerDepth {
            winnerCount++
        }
    }

    /* no embed carries a codec, or the shallowest ones tie and promote none: the method the caller found in the method set is declared on targetType */
    if 1 != winnerCount {
        return targetType, 0, true
    }

    return winner, winnerDepth, true
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

/* isRequired lists a field required only when the validator turns its absence into a 400: notEmpty on every kind, notBlank on a pointer or a genuine string. On the other kinds notBlank makes the field unsatisfiable, which applyValidation lists required. */
func isRequired(field reflect.StructField, schema *Schema) bool {
    /* an omitted pointer or interface field dereferences to nothing, which notBlank rejects as "this field is required" */
    absenceIsNil := reflect.Ptr == field.Type.Kind() || reflect.Interface == field.Type.Kind()

    for _, rule := range parsedTagRules(field.Tag.Get("validate")) {
        name := rule.name

        if "notEmpty" == name {
            /* notEmpty on a non-pointer fixed-length array never rejects, so the field is not required; a pointer to one is, since the validator rejects nil */
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

/* fixedArrayNotEmptyIsUnsatisfiable reports whether a field is a zero-length fixed array carrying notEmpty, for which the validator's length check can never pass, so no payload the spec advertises is ever accepted. */
func fixedArrayNotEmptyIsUnsatisfiable(field reflect.StructField) bool {
    fieldType := dereferencedType(field.Type)

    if reflect.Array != fieldType.Kind() || 0 != fieldType.Len() {
        return false
    }

    for _, rule := range parsedTagRules(field.Tag.Get("validate")) {
        name := rule.name
        if "notEmpty" == name {
            return true
        }
    }

    return false
}

/* fixedArrayNotEmptyIsVacuous reports whether a field is a fixed-length array carrying notEmpty, whose length check can never fail; the type is dereferenced as the unsatisfiable sibling dereferences it. */
func fixedArrayNotEmptyIsVacuous(field reflect.StructField) bool {
    fieldType := dereferencedType(field.Type)

    if reflect.Array != fieldType.Kind() || 1 > fieldType.Len() {
        return false
    }

    for _, rule := range parsedTagRules(field.Tag.Get("validate")) {
        name := rule.name
        if "notEmpty" == name {
            return true
        }
    }

    return false
}

/* byte and date-time mark a non-string value rendered as a string, which the validator's string constraints refuse; email annotates a genuine string and the later string facets still apply to it. */
func isStructuralStringFormat(format string) bool {
    return "" != format && "email" != format
}

/* parseBoundStrict must accept exactly the bounds the validator's parseIntStrict accepts, both being strconv.Atoi. Change one only with the other and TestLockstepNumericBoundAgreesWithValidator. */
func parseBoundStrict(valueString string) (int, bool) {
    parsed, err := strconv.Atoi(valueString)
    if nil != err {
        return 0, false
    }

    return parsed, true
}

/* applyValidation reads a field's validate tag onto its schema and reports whether the rule set rejects every value — the caller's cue to list the field required, since the absent zero value goes through the same rules. */
func applyValidation(schema *Schema, validateTag string, components map[string]*Schema) bool {
    if true == tagHasInvalidSyntax(validateTag) {
        /* the validator refuses a malformed tag before examining any value, while splitRule here is lenient, so the syntax is checked first and the field advertised unsatisfiable */
        markFieldUnsatisfiable(schema)
        return true
    }

    if true == tagHasBareParameterizedConstraint(validateTag) {
        /* a parameterized rule named without parameters fails closed in the validator, so the field accepts no value */
        markFieldUnsatisfiable(schema)
        return true
    }

    if true == tagHasUnconsumedParameterizedParams(validateTag) {
        /* a parameterizable constraint given parameters without its recognized key fails closed in the validator, so the field accepts no value */
        markFieldUnsatisfiable(schema)
        return true
    }

    if true == tagHasEmptyRegexPattern(validateTag) {
        /* the validator refuses an empty regex pattern at construction, so the field accepts no value */
        markFieldUnsatisfiable(schema)
        return true
    }

    if "" != schema.Ref || nil != schema.AllOf {
        /* the reference is resolved to the component it names, since notEmpty rejects a struct but measures a named collection's length */
        referencedKind := referencedCollectionKind(schema, components)

        if true == tagRejectsReferencedValue(validateTag, referencedKind) || true == tagHasParamsOnNonParameterizable(validateTag) || true == tagRejectsAllViaNumericBound(validateTag) {
            markFieldUnsatisfiable(schema)

            return true
        }

        if true == tagRefusesNonStringValue(validateTag) {
            /* min/max and the format constraints refuse the referenced value but pass a nil pointer, so a nullable field keeps only its null */
            if true == schema.Nullable {
                applyEmptyValueSpace(schema)

                return false
            }

            markFieldUnsatisfiable(schema)

            return true
        }

        if "" != referencedKind && true == tagRequiresNonEmptyValue(validateTag) {
            /* a referenced named collection under notEmpty gets the inline floor, stamped beside the schema and moved onto the reference by addFieldProperty */
            schema.Nullable = false
            applyReferencedCollectionFloor(schema, referencedKind)
        }

        return false
    }

    patterns := []string{}
    rejectsAll := false
    emptyValueSpace := false

    /* a malformed or negative bound fails the rule closed before any value is examined, so this reject-all is decided before the per-kind cases */
    if true == tagRejectsAllViaNumericBound(validateTag) {
        rejectsAll = true
    }

    for _, rule := range parsedTagRules(validateTag) {
        name, params := rule.name, rule.params

        switch name {
        case "email":
            if 0 != len(params) {
                /* parameters a non-parameterizable constraint cannot consume fail the rule closed in the validator */
                rejectsAll = true

                continue
            }
            /* only a genuine string is annotated; every other shape is refused by the constraint while a nil pointer passes */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                schema.Format = "email"
            } else if "" != schema.Type {
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "min":
            /* the length constraints refuse every non-string value and pass a nil pointer; a malformed bound and a bare min are decided before the switch */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk && 0 <= parsed {
                    value := parsed
                    /* raise-only, so min=0 cannot lower an earlier notEmpty or notBlank floor of 1 */
                    if nil == schema.MinLength || value > *schema.MinLength {
                        schema.MinLength = &value
                    }
                }
            } else if "" != schema.Type {
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "max":
            /* max measures a genuine string only, so on any other shape every non-null value is refused */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk && 0 <= parsed {
                    value := parsed
                    /* tighten-only: a lower ceiling another rule set must not be raised by a looser max */
                    if nil == schema.MaxLength || value < *schema.MaxLength {
                        schema.MaxLength = &value
                    }
                }
            } else if "" != schema.Type {
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        /* only "regex" is modelled: the validator registers no "pattern" constraint, and a name the mirror does not model is left out of the schema, which under-constrains rather than contradicts */
        case "regex":
            /* the empty pattern is decided before the switch, so only a non-empty one reaches here */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                pattern := patternParam(params)
                if _, compileErr := regexp.Compile(pattern); nil != compileErr {
                    /* an uncompilable pattern is not emitted; the validator accepts the empty string and nil before its invalid-pattern branch, so the field is advertised as maxLength 0 */
                    zeroLength := 0
                    if nil == schema.MaxLength || 0 < *schema.MaxLength {
                        schema.MaxLength = &zeroLength
                    }
                } else {
                    patterns = append(patterns, pattern)
                }
            } else if "" != schema.Type {
                /* the non-string refusal precedes the invalid-pattern branch, so every non-null non-string value is refused */
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "alpha":
            if 0 != len(params) {
                /* parameters a non-parameterizable constraint cannot consume fail the rule closed in the validator */
                rejectsAll = true

                continue
            }
            /* the validator accepts the empty string before its anchored class, so the class is advertised with * rather than +; a non-string shape is refused while nil passes */
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                patterns = append(patterns, "^[a-zA-Z]*$")
            } else if "" != schema.Type {
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "numeric":
            if 0 != len(params) {
                rejectsAll = true

                continue
            }
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                patterns = append(patterns, "^[0-9]*$")
            } else if "" != schema.Type {
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "alphanumeric":
            if 0 != len(params) {
                rejectsAll = true

                continue
            }
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                patterns = append(patterns, "^[a-zA-Z0-9]*$")
            } else if "" != schema.Type {
                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "notBlank":
            if 0 != len(params) {
                rejectsAll = true

                continue
            }
            /* notBlank refuses null and every non-string value, so on a non-string shape the field accepts nothing; on a string it rejects the empty value, advertised as minLength 1 over any min=0. Nullable is cleared either way. */
            schema.Nullable = false
            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if nil == schema.MinLength || 1 > *schema.MinLength {
                    minLength := 1
                    schema.MinLength = &minLength
                }
            } else if "" != schema.Type {
                rejectsAll = true
            }
        case "notEmpty":
            if 0 != len(params) {
                /* parameters a non-parameterizable constraint cannot consume fail the rule closed in the validator */
                rejectsAll = true

                continue
            }
            /* notEmpty rejects null and a zero length, so nullable is cleared and the floor stamped per shape; a struct or any other kind is rejected outright and advertised unsatisfiable */
            schema.Nullable = false
            switch schema.Type {
            case "string":
                if "date-time" == schema.Format {
                    /* a time.Time is a struct to notEmpty and rejected outright, although it is rendered as a date-time string */
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
                    /* a map is an object with additionalProperties, floored by minProperties 1 */
                    if nil == schema.MinProperties {
                        minProperties := 1
                        schema.MinProperties = &minProperties
                    }
                } else {
                    /* an inline struct is rejected outright by notEmpty */
                    rejectsAll = true
                }
            case "":
                /* an interface field carries no type and the validator judges the DECODED dynamic value — a non-empty string or map passes notEmpty — so the field stays satisfiable and unconstrained */
            default:
                /* notEmpty rejects a number or a boolean outright, so the scalar is advertised with an empty value space */
                rejectsAll = true
            }
        case "greaterThan":
            if "integer" == schema.Type || "number" == schema.Type {
                /* greaterThan rejects a null pointer, so nullable is cleared; only a parseable bound reaches the facet, a negative one being legitimate here */
                schema.Nullable = false
                exclusive := true
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk {
                    /* the validator enforces every duplicate rule, so the effective floor is the highest bound: raise-only, mirroring the min/max length branches */
                    value := float64(parsed)
                    if nil == schema.Minimum || value > *schema.Minimum {
                        schema.Minimum = &value
                        schema.ExclusiveMinimum = &exclusive
                    }
                }
            } else if "" != schema.Type {
                /* greaterThan rejects a non-numeric value outright; an interface field stays satisfiable, since the validator judges the decoded value */
                rejectsAll = true
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {
                /* lessThan rejects a null pointer, so nullable is cleared; only a parseable bound reaches the facet, a negative one being legitimate here */
                schema.Nullable = false
                exclusive := true
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk {
                    /* the validator enforces every duplicate rule, so the effective ceiling is the lowest bound: tighten-only, mirroring the min/max length branches */
                    value := float64(parsed)
                    if nil == schema.Maximum || value < *schema.Maximum {
                        schema.Maximum = &value
                        schema.ExclusiveMaximum = &exclusive
                    }
                }
            } else if "" != schema.Type {
                /* lessThan rejects a non-numeric value outright; an interface field stays satisfiable, since the validator judges the decoded value */
                rejectsAll = true
            }
        }
    }

    if 1 == len(patterns) {
        schema.Pattern = patterns[0]
    } else if 1 < len(patterns) {
        /* one OpenAPI pattern holds one expression, so every regex rule becomes an allOf member the client satisfies together */
        for _, pattern := range patterns {
            schema.AllOf = append(schema.AllOf, &Schema{Pattern: pattern})
        }
    }

    if true == rejectsAll {
        markFieldUnsatisfiable(schema)

        return true
    }

    if true == emptyValueSpace {
        /* the field's non-null value space is empty (a nullable non-string shape under a string-only constraint) but the validator still accepts null, so contradict only the value while preserving the nullable advertisement */
        applyEmptyValueSpace(schema)
    }

    return false
}

/* referencedCollectionKind reports "array" for a named slice or array component, "object" for a named map, and "" otherwise, the stricter struct reading. The resolution is bounded three ways (reference-only chains, keys already followed, the generator's nesting bound), so it terminates on any components map. */
func referencedCollectionKind(schema *Schema, components map[string]*Schema) string {
    reference := schema.Ref
    if "" == reference {
        /* a nullable field carries its reference as the single member of an allOf wrapper (withNullable), the reference itself having no room for the nullable flag beside it */
        for _, member := range schema.AllOf {
            if "" != member.Ref {
                reference = member.Ref

                break
            }
        }
    }

    followed := map[string]bool{}
    for hop := 0; hop < maximumSchemaDepth; hop++ {
        if "" == reference {
            return ""
        }

        key := strings.TrimPrefix(reference, "#/components/schemas/")
        if true == followed[key] {
            return ""
        }
        followed[key] = true

        resolved, exists := components[key]
        if false == exists || nil == resolved {
            return ""
        }

        if "" != resolved.Ref {
            reference = resolved.Ref

            continue
        }

        if "array" == resolved.Type {
            return "array"
        }

        /* a map renders as an object carrying additionalProperties; a struct renders as an object carrying a fixed property set, and the placeholder a builder claims a key with carries neither */
        if "object" == resolved.Type && nil != resolved.AdditionalProperties {
            return "object"
        }

        return ""
    }

    return ""
}

/* referencedComponentRejectsAll reports whether a field's schema stands for a component markFieldUnsatisfiable stamped (minProperties above maxProperties). A component unsatisfiable only through a property of its own is not recognized. */
func referencedComponentRejectsAll(schema *Schema, components map[string]*Schema) bool {
    resolved := schema

    reference := schema.Ref
    if "" == reference {
        for _, member := range schema.AllOf {
            if "" != member.Ref {
                reference = member.Ref

                break
            }
        }
    }

    if "" != reference {
        key := strings.TrimPrefix(reference, "#/components/schemas/")
        component, exists := components[key]
        if false == exists || nil == component {
            return false
        }
        resolved = component
    }

    if nil == resolved.MinProperties || nil == resolved.MaxProperties {
        return false
    }

    return *resolved.MinProperties > *resolved.MaxProperties
}

/* applyReferencedCollectionFloor stamps the notEmpty floor for a referenced collection, raise-only and per shape exactly as the inline branches do: minItems 1 for an array, minProperties 1 for a map. */
func applyReferencedCollectionFloor(schema *Schema, referencedKind string) {
    floor := 1

    if "array" == referencedKind {
        if nil == schema.MinItems {
            schema.MinItems = &floor
        }

        return
    }

    if nil == schema.MinProperties {
        schema.MinProperties = &floor
    }
}

/* liftValidationFacetsOntoReference moves a floor stamped beside a $ref into an allOf, since a facet beside a $ref binds nothing; a nullable reference already in an allOf takes the floor as one more member. */
func liftValidationFacetsOntoReference(schema *Schema) {
    if nil == schema.MinItems && nil == schema.MinProperties {
        return
    }

    if "" == schema.Ref && nil == schema.AllOf {
        return
    }

    facets := &Schema{MinItems: schema.MinItems, MinProperties: schema.MinProperties}
    schema.MinItems = nil
    schema.MinProperties = nil

    if "" != schema.Ref {
        schema.AllOf = []*Schema{{Ref: schema.Ref}, facets}
        schema.Ref = ""

        return
    }

    schema.AllOf = append(schema.AllOf, facets)
}

/* markFieldUnsatisfiable advertises a schema no value, null included, satisfies, for a validator that rejects the field outright: it clears Nullable and contradicts the value space. */
func markFieldUnsatisfiable(schema *Schema) {
    schema.Nullable = false
    applyEmptyValueSpace(schema)
}

/* applyEmptyValueSpace contradicts the non-null value space without touching Nullable, for a validator that still accepts null: an impossible length, range, item count or property count per shape, and the object contradiction under allOf for a $ref. */
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
        /* an empty enum is invalid under OpenAPI 3.0, so a boolean gets two contradictory single-value enums under allOf */
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
        /* a map (object with additionalProperties) or an inline struct (object with a fixed property set) is contradicted at the object level: a value cannot have both at least one and at most zero properties */
        minProperties := 1
        maxProperties := 0
        schema.MinProperties = &minProperties
        schema.MaxProperties = &maxProperties
    case "":
        /* a $ref may name a collection component, on which draft-04's minProperties and maxProperties bind nothing, so the type-agnostic not: {} is written instead */
        contradiction := &Schema{Not: &Schema{}}
        if "" != schema.Ref {
            schema.AllOf = []*Schema{{Ref: schema.Ref}, contradiction}
            schema.Ref = ""
        } else if nil != schema.AllOf {
            schema.AllOf = append(schema.AllOf, contradiction)
        } else {
            /* an interface field carries neither a type nor a $ref, so there is nothing to conjoin with: the refusal is written directly */
            schema.Not = &Schema{}
        }
    }
}

/* tagHasInvalidSyntax reports whether a validate tag is malformed the way parseValidationTag refuses before any value is examined. It reuses the validator's split helpers, so a valid tag never trips it. */
func tagHasInvalidSyntax(validateTag string) bool {
    parts := splitRules(validateTag)

    ruleCount := 0
    for _, rule := range parts {
        part := strings.TrimSpace(rule)
        if "" == part {
            continue
        }
        ruleCount++

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

    /* splitRules answers nil for the empty tag and the skip marker, which the validator's own tag door skips before parsing — so a non-nil part list with no rule in it is exactly the shape parseValidationTag refuses with 0 == len(rules) */
    if nil != parts && 0 == ruleCount {
        return true
    }

    return false
}

/* tagHasParamsOnNonParameterizable reports whether a constraint that takes no parameters carries some, which the validator fails closed; it is read before applyValidation returns early for a $ref or allOf schema. */
func tagHasParamsOnNonParameterizable(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name, params := rule.name, rule.params
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

/* tagHasUnconsumedParameterizedParams reports whether a parameterizable constraint carries parameters without its recognized key ("value", or "pattern"/"value" for regex), which the validator fails closed. */
func tagHasUnconsumedParameterizedParams(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name, params := rule.name, rule.params
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

/* tagHasBareParameterizedConstraint reports whether min, max, greaterThan, lessThan or regex is named with no parameters, which the validator fails closed. */
func tagHasBareParameterizedConstraint(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name, params := rule.name, rule.params
        if 0 != len(params) {
            continue
        }

        switch name {
        case "min", "max", "greaterThan", "lessThan", "regex":
            return true
        }
    }

    return false
}

/* tagHasEmptyRegexPattern reports whether a regex rule names an explicitly empty pattern, which the validator refuses at construction; the bare and unconsumed-key cases are answered before it. */
func tagHasEmptyRegexPattern(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name, params := rule.name, rule.params
        if "regex" != name || 0 == len(params) {
            continue
        }

        if "" == patternParam(params) {
            return true
        }
    }

    return false
}

/* tagRejectsReferencedValue reports whether a constraint rejects both the value a $ref stands for and its null: greaterThan, lessThan and notBlank always, notEmpty only for a struct component. The nil-skipping refusals belong to tagRefusesNonStringValue. */
func tagRejectsReferencedValue(validateTag string, referencedKind string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name := rule.name
        switch name {
        case "greaterThan", "lessThan", "notBlank":
            return true
        case "notEmpty":
            if "" == referencedKind {
                return true
            }
        }
    }

    return false
}

/* tagRefusesNonStringValue reports whether a string-only constraint (min, max, email, alpha, numeric, alphanumeric, regex) refuses every non-string value while passing a nil pointer. */
func tagRefusesNonStringValue(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name := rule.name
        switch name {
        case "min", "max", "email", "alpha", "numeric", "alphanumeric", "regex":
            return true
        }
    }

    return false
}

/* reports whether a validate tag carries a bare notEmpty, the constraint that floors the length of the collection a $ref stands for. A notEmpty carrying parameters never reaches this question: the validator fails such a rule closed and tagHasParamsOnNonParameterizable has already answered for it. */
func tagRequiresNonEmptyValue(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name := rule.name
        if "notEmpty" == name {
            return true
        }
    }

    return false
}

/* tagRejectsAllViaNumericBound reports whether a min, max, greaterThan or lessThan bound is refused at construction, being no integer or a negative min/max, which rejects every value, nil included. A negative greaterThan/lessThan bound is legitimate. */
func tagRejectsAllViaNumericBound(validateTag string) bool {
    for _, rule := range parsedTagRules(validateTag) {
        name, params := rule.name, rule.params
        if "min" != name && "max" != name && "greaterThan" != name && "lessThan" != name {
            continue
        }

        valueString, exists := params["value"]
        if false == exists {
            continue
        }

        parsed, parsedOk := parseBoundStrict(valueString)
        if false == parsedOk {
            return true
        }

        if 0 > parsed && ("min" == name || "max" == name) {
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

/* parsedTagRule is one rule of a validate tag as splitRule reads it; the params map is shared by every reader of the memo and is never written after the parse. */
type parsedTagRule struct {
    name   string
    params map[string]string
}

/* parsedTagRulesCache memoizes the parse of a validate tag, which nine predicates read per field on every spec request. Nothing evicts, so a program that builds types with reflect.StructOf per request must not build their validate tags per request. */
var parsedTagRulesCache sync.Map

func parsedTagRules(validateTag string) []parsedTagRule {
    if cached, exists := parsedTagRulesCache.Load(validateTag); true == exists {
        return cached.([]parsedTagRule)
    }

    var rules []parsedTagRule
    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
        rules = append(rules, parsedTagRule{name: name, params: params})
    }

    /* LoadOrStore rather than Store so a concurrent first touch settles on ONE parse, the rules and their maps being shared by identity */
    stored, _ := parsedTagRulesCache.LoadOrStore(validateTag, rules)

    return stored.([]parsedTagRule)
}

func splitRules(validateTag string) []string {
    trimmed := strings.TrimSpace(validateTag)
    if "" == trimmed || "-" == trimmed {
        return nil
    }

    return splitTopLevelRules(trimmed)
}

/* charClassScanner tracks a regex character class so its members read as literals: a ']' first in the class is a literal, and a nested [:name:] class is tracked apart from the enclosing one. */
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
            /* RE2 recognizes only [: :] inside a class; [. .] and [= =] are literals, as in the validator's scanner */
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

        /* a ']' closing no class is a literal to RE2, as the validator's hasBalancedBrackets reads it */
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
