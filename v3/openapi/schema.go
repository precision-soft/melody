package openapi

import (
    "encoding/json"
    "reflect"
    "regexp"
    "strconv"
    "strings"
    "time"
)

var timeType = reflect.TypeOf(time.Time{})
var jsonMarshalerInterfaceType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

const maximumSchemaDepth = 64

var schemaDepthLimitDescription = "schema generation stopped at the maximum nesting depth of " +
    strconv.Itoa(maximumSchemaDepth) +
    " levels; this position is left undescribed"

func depthLimitSchema() *Schema {
    return &Schema{Description: schemaDepthLimitDescription}
}

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

func embedTagRejectsAll(field reflect.StructField) bool {
    validateTag := field.Tag.Get("validate")

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

    embedCount := make(map[reflect.Type]int)

    ownCandidatesByName := make(map[string][]embeddedCandidate)
    var ownOrder []string
    var embedQueue []reflect.Type
    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if true == isPromotedEmbed(field) {

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

    if true == fixedArrayNotEmptyIsVacuous(field) && nil == propertySchema.MaxItems {
        propertySchema.MinItems = nil
    }

    liftValidationFacetsOntoReference(propertySchema)

    if true == fixedArrayNotEmptyIsUnsatisfiable(field) {
        markFieldUnsatisfiable(propertySchema)
    }

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

func quotedScalarSchema(original *Schema, valueKind reflect.Kind, isPointer bool) *Schema {
    quoted := &Schema{Type: "string", Nullable: original.Nullable}

    if true == scalarSchemaRejectsAll(original) {

        if true == original.Nullable {
            quoted.Enum = &[]any{"null"}

            return quoted
        }

        applyEmptyValueSpace(quoted)

        return quoted
    }

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

func zeroValueRejectsAbsentProperty(field reflect.StructField, schema *Schema) bool {
    isPointer := reflect.Ptr == field.Type.Kind()

    for _, rule := range splitRules(field.Tag.Get("validate")) {
        name, params := splitRule(rule)

        switch name {
        case "min":

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

func isRequired(field reflect.StructField, schema *Schema) bool {

    absenceIsNil := reflect.Ptr == field.Type.Kind() || reflect.Interface == field.Type.Kind()

    for _, rule := range splitRules(field.Tag.Get("validate")) {
        name, _ := splitRule(rule)

        if "notEmpty" == name {

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

func fixedArrayNotEmptyIsUnsatisfiable(field reflect.StructField) bool {
    fieldType := dereferencedType(field.Type)

    if reflect.Array != fieldType.Kind() || 0 != fieldType.Len() {
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

func fixedArrayNotEmptyIsVacuous(field reflect.StructField) bool {
    fieldType := dereferencedType(field.Type)

    if reflect.Array != fieldType.Kind() || 1 > fieldType.Len() {
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

func isStructuralStringFormat(format string) bool {
    return "" != format && "email" != format
}

func parseBoundStrict(valueString string) (int, bool) {
    parsed, err := strconv.Atoi(valueString)
    if nil != err {
        return 0, false
    }

    return parsed, true
}

func applyValidation(schema *Schema, validateTag string, components map[string]*Schema) bool {
    if true == tagHasInvalidSyntax(validateTag) {

        markFieldUnsatisfiable(schema)
        return true
    }

    if true == tagHasBareParameterizedConstraint(validateTag) {

        markFieldUnsatisfiable(schema)
        return true
    }

    if true == tagHasUnconsumedParameterizedParams(validateTag) {

        markFieldUnsatisfiable(schema)
        return true
    }

    if true == tagHasEmptyRegexPattern(validateTag) {

        markFieldUnsatisfiable(schema)
        return true
    }

    if "" != schema.Ref || nil != schema.AllOf {

        referencedKind := referencedCollectionKind(schema, components)

        if true == tagRejectsReferencedValue(validateTag, referencedKind) || true == tagHasParamsOnNonParameterizable(validateTag) || true == tagRejectsAllViaNumericBound(validateTag) {
            markFieldUnsatisfiable(schema)

            return true
        }

        if true == tagRefusesNonStringValue(validateTag) {

            if true == schema.Nullable {
                applyEmptyValueSpace(schema)

                return false
            }

            markFieldUnsatisfiable(schema)

            return true
        }

        if "" != referencedKind && true == tagRequiresNonEmptyValue(validateTag) {

            schema.Nullable = false
            applyReferencedCollectionFloor(schema, referencedKind)
        }

        return false
    }

    patterns := []string{}
    rejectsAll := false
    emptyValueSpace := false

    if true == tagRejectsAllViaNumericBound(validateTag) {
        rejectsAll = true
    }

    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)

        switch name {
        case "email":
            if 0 != len(params) {

                rejectsAll = true

                continue
            }

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

            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk && 0 <= parsed {
                    value := parsed

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

            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk && 0 <= parsed {
                    value := parsed

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

        case "regex":

            if "string" == schema.Type && false == isStructuralStringFormat(schema.Format) {
                pattern := patternParam(params)
                if _, compileErr := regexp.Compile(pattern); nil != compileErr {

                    zeroLength := 0
                    if nil == schema.MaxLength || 0 < *schema.MaxLength {
                        schema.MaxLength = &zeroLength
                    }
                } else {
                    patterns = append(patterns, pattern)
                }
            } else if "" != schema.Type {

                if true == schema.Nullable {
                    emptyValueSpace = true
                } else {
                    rejectsAll = true
                }
            }
        case "alpha":
            if 0 != len(params) {

                rejectsAll = true

                continue
            }

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

                rejectsAll = true

                continue
            }

            schema.Nullable = false
            switch schema.Type {
            case "string":
                if "date-time" == schema.Format {

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

                    if nil == schema.MinProperties {
                        minProperties := 1
                        schema.MinProperties = &minProperties
                    }
                } else {

                    rejectsAll = true
                }
            case "":

            default:

                rejectsAll = true
            }
        case "greaterThan":
            if "integer" == schema.Type || "number" == schema.Type {

                schema.Nullable = false
                exclusive := true
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk {

                    value := float64(parsed)
                    if nil == schema.Minimum || value > *schema.Minimum {
                        schema.Minimum = &value
                        schema.ExclusiveMinimum = &exclusive
                    }
                }
            } else if "" != schema.Type {

                rejectsAll = true
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {

                schema.Nullable = false
                exclusive := true
                if parsed, parsedOk := parseBoundStrict(params["value"]); true == parsedOk {

                    value := float64(parsed)
                    if nil == schema.Maximum || value < *schema.Maximum {
                        schema.Maximum = &value
                        schema.ExclusiveMaximum = &exclusive
                    }
                }
            } else if "" != schema.Type {

                rejectsAll = true
            }
        }
    }

    if 1 == len(patterns) {
        schema.Pattern = patterns[0]
    } else if 1 < len(patterns) {

        for _, pattern := range patterns {
            schema.AllOf = append(schema.AllOf, &Schema{Pattern: pattern})
        }
    }

    if true == rejectsAll {
        markFieldUnsatisfiable(schema)

        return true
    }

    if true == emptyValueSpace {

        applyEmptyValueSpace(schema)
    }

    return false
}

func referencedCollectionKind(schema *Schema, components map[string]*Schema) string {
    reference := schema.Ref
    if "" == reference {

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

        if "object" == resolved.Type && nil != resolved.AdditionalProperties {
            return "object"
        }

        return ""
    }

    return ""
}

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

func markFieldUnsatisfiable(schema *Schema) {
    schema.Nullable = false
    applyEmptyValueSpace(schema)
}

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

        minProperties := 1
        maxProperties := 0
        schema.MinProperties = &minProperties
        schema.MaxProperties = &maxProperties
    case "":

        contradiction := &Schema{Not: &Schema{}}
        if "" != schema.Ref {
            schema.AllOf = []*Schema{{Ref: schema.Ref}, contradiction}
            schema.Ref = ""
        } else if nil != schema.AllOf {
            schema.AllOf = append(schema.AllOf, contradiction)
        } else {

            schema.Not = &Schema{}
        }
    }
}

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

    if nil != parts && 0 == ruleCount {
        return true
    }

    return false
}

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

func tagHasBareParameterizedConstraint(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
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

func tagHasEmptyRegexPattern(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)
        if "regex" != name || 0 == len(params) {
            continue
        }

        if "" == patternParam(params) {
            return true
        }
    }

    return false
}

func tagRejectsReferencedValue(validateTag string, referencedKind string) bool {
    for _, rule := range splitRules(validateTag) {
        name, _ := splitRule(rule)
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

func tagRefusesNonStringValue(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, _ := splitRule(rule)
        switch name {
        case "min", "max", "email", "alpha", "numeric", "alphanumeric", "regex":
            return true
        }
    }

    return false
}

func tagRequiresNonEmptyValue(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, _ := splitRule(rule)
        if "notEmpty" == name {
            return true
        }
    }

    return false
}

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

func splitRules(validateTag string) []string {
    trimmed := strings.TrimSpace(validateTag)
    if "" == trimmed || "-" == trimmed {
        return nil
    }

    return splitTopLevelRules(trimmed)
}

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
