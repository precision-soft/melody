package openapi

import (
    "fmt"
    "reflect"
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

        return withNullable(&Schema{Type: "array", Items: buildSchema(targetType.Elem(), components, names, visited)}, nullable)
    case reflect.Map:
        return withNullable(&Schema{Type: "object", AdditionalProperties: buildSchema(targetType.Elem(), components, names, visited)}, nullable)
    case reflect.Struct:
        return structSchemaReference(targetType, components, names, visited, nullable)
    default:
        return withNullable(&Schema{}, nullable)
    }
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
    collectStructFields(structType, components, names, visited, schema.Properties, &required)

    if 0 == len(schema.Properties) {
        schema.Properties = nil
    }

    if 0 < len(required) {
        schema.Required = required
    }

    return schema
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
            embeddedType := dereferencedStructType(field.Type)
            if true == embeddedSeen[embeddedType] {
                continue
            }
            if 0 == embedCount[embeddedType] {
                embedQueue = append(embedQueue, embeddedType)
            }
            embedCount[embeddedType]++
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
                    childType := dereferencedStructType(field.Type)
                    if true == embeddedSeen[childType] {
                        continue
                    }
                    if 0 == nextCount[childType] {
                        nextLevel = append(nextLevel, childType)
                    }
                    nextCount[childType] += multiplicity
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
    properties[jsonName] = propertySchema

    if true == isRequired(field.Tag.Get("validate")) || true == pointerBoundRequiresPresence(field) {
        *required = append(*required, jsonName)
    }
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

func isRequired(validateTag string) bool {
    for _, rule := range splitRules(validateTag) {
        name, _ := splitRule(rule)
        if "notBlank" == name || "notEmpty" == name {
            return true
        }
    }

    return false
}

func parseLeadingInt(valueString string) (int64, bool) {
    var result int64
    if _, scanErr := fmt.Sscanf(valueString, "%d", &result); nil != scanErr {
        return 0, false
    }

    return result, true
}

func applyValidation(schema *Schema, validateTag string) {
    if "" != schema.Ref || nil != schema.AllOf {
        /* @important a $ref (or a nullable allOf-wrapped $ref) always denotes a struct component, and the validator rejects a struct value outright for notEmpty (constraint_not_empty.go default branch) and for greaterThan/lessThan ("value must be numeric", constraint_greater_than.go/constraint_less_than.go default branch); such a tag makes the field unsatisfiable server-side, so advertise it as such rather than as a satisfiable object a client would trust. No length/numeric facet otherwise attaches to a $ref, so there is nothing else to apply here. */
        if true == tagRejectsStruct(validateTag) {
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

    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)

        switch name {
        case "email":
            /* @important only set the email format on a genuine string whose format slot is free, so a structural format such as byte (for a []byte field) is preserved and the spec does not advertise an email constraint the validator cannot enforce on non-string values */
            if "string" == schema.Type && "" == schema.Format {
                schema.Format = "email"
            }
        case "min":
            if "string" == schema.Type {
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
                        /* @important a malformed min value (e.g. min=abc) makes the validator fail the whole field closed (parseIntStrict rejects it, post-CR70), so the field accepts no value — flag it unsatisfiable rather than advertise a passable default the client would trust */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less min constraint is enforced as minLength 1 by the validator, so the spec must advertise the same bound */
                    defaultMinLength := 1
                    schema.MinLength = &defaultMinLength
                }
            }
        case "max":
            if "string" == schema.Type {
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        value := int(parsed)
                        if 0 > value {
                            /* @important a negative max makes MaxLength.Validate (len > max) reject every value including the empty string, so the field accepts nothing — flag it unsatisfiable rather than advertise maxLength 0 (which would advertise "" as valid) */
                            rejectsAll = true
                        } else {
                            schema.MaxLength = &value
                        }
                    } else {
                        /* @important a malformed max value makes the validator fail the whole field closed (post-CR70), so flag it unsatisfiable */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less max constraint is enforced as maxLength 100 by the validator, so the spec must advertise the same bound */
                    defaultMaxLength := 100
                    schema.MaxLength = &defaultMaxLength
                }
            } else if "integer" == schema.Type || "number" == schema.Type || "boolean" == schema.Type {
                /* @important MaxLength.Validate is not string-only: it stringifies any dereferenced value with %v and rejects it when len > max, so `max` is enforced on integer/number/boolean fields too. The shortest possible stringification is 1 character for an integer/number (e.g. "0") and 4 for a boolean ("true"), so a bound below that minimum — or a negative or malformed bound, which fails the field closed — admits no non-null value. There is no exact OpenAPI numeric facet for a stringified-length ceiling, so a satisfiable bound (at or above the minimum) is left unconstrained rather than mis-advertised; only the empty-value-space corner is advertised. A nil pointer passes MaxLength (dereferenceValue returns ok=false, Validate returns nil), so for a nullable field null stays valid and only the non-null space is empty (emptyValueSpace); a non-nullable field accepts nothing at all (rejectsAll). */
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
        case "regex", "pattern":
            if "string" == schema.Type {
                patterns = append(patterns, patternParam(params))
            }
        case "alpha":
            /* @important the validator enforces these character classes with an anchored pattern but short-circuits on an empty string (it accepts ""), so advertise the class with a * quantifier — which also matches "" — rather than + which would reject the "" the validator accepts */
            if "string" == schema.Type {
                patterns = append(patterns, "^[a-zA-Z]*$")
            }
        case "numeric":
            if "string" == schema.Type {
                patterns = append(patterns, "^[0-9]*$")
            }
        case "alphanumeric":
            if "string" == schema.Type {
                patterns = append(patterns, "^[a-zA-Z0-9]*$")
            }
        case "notBlank":
            /* @important notBlank rejects a null pointer for a field of any kind (dereferenceValue returns ok=false), so the spec must never advertise the field as nullable regardless of its generated type — clear nullable unconditionally (mirroring notEmpty), not only on the string path, so a *int/*bool/*float64/*[]T/*map/*struct field is not advertised as accepting null the validator rejects. The length floor is string-only: for a string, notBlank also rejects an empty (or whitespace-only) value, so advertise minLength 1 (the OpenAPI required list only means the key is present, so an empty value would still satisfy it; a client must not send "" against the spec and then be rejected). An explicit min >= 1 in either tag order still wins, but a degenerate min=0 is raised to 1 because notBlank forbids the empty value. The whitespace-only rejection cannot be expressed by minLength. For non-string kinds notBlank accepts any non-null value (its %v stringification is non-blank), so no length/items/properties floor is advertised. */
            schema.Nullable = false
            if "string" == schema.Type {
                if nil == schema.MinLength || 1 > *schema.MinLength {
                    minLength := 1
                    schema.MinLength = &minLength
                }
            }
        case "notEmpty":
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
                        value := float64(parsed)
                        schema.Minimum = &value
                        schema.ExclusiveMinimum = &exclusive
                    } else {
                        /* @important a malformed greaterThan value makes the validator fail the whole field closed (post-CR70), so flag it unsatisfiable rather than advertise the > 0 default */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less greaterThan constraint is enforced as > 0 by the validator, so the spec must advertise the same bound */
                    value := float64(0)
                    schema.Minimum = &value
                    schema.ExclusiveMinimum = &exclusive
                }
            } else {
                /* @important greaterThan's validator rejects a non-numeric value outright ("value must be numeric", constraint_greater_than.go default branch), so a string/boolean/array/object field carrying greaterThan is unsatisfiable server-side; advertise it as such rather than as an unconstrained value a client would trust */
                rejectsAll = true
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {
                /* @important the validator rejects a null pointer for greaterThan/lessThan, so the spec must not advertise the field as nullable */
                schema.Nullable = false
                exclusive := true
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parsedOk := parseLeadingInt(valueString); true == parsedOk {
                        value := float64(parsed)
                        schema.Maximum = &value
                        schema.ExclusiveMaximum = &exclusive
                    } else {
                        /* @important a malformed lessThan value makes the validator fail the whole field closed (post-CR70), so flag it unsatisfiable rather than advertise the < 0 default */
                        rejectsAll = true
                    }
                } else {
                    /* @important a value-less lessThan constraint is enforced as < 0 by the validator, so the spec must advertise the same bound */
                    value := float64(0)
                    schema.Maximum = &value
                    schema.ExclusiveMaximum = &exclusive
                }
            } else {
                /* @important lessThan's validator rejects a non-numeric value outright ("value must be numeric", constraint_less_than.go default branch), so a string/boolean/array/object field carrying lessThan is unsatisfiable server-side; advertise it as such rather than as an unconstrained value a client would trust */
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

/* @important markFieldUnsatisfiable advertises a schema no value — null included — can satisfy, mirroring a validator that rejects the field outright for a malformed numeric/length tag, a negative max (both fail the field closed post-CR70), or a constraint applied to a kind the validator rejects (notEmpty on a struct, greaterThan/lessThan on a non-numeric — CR #74). It clears Nullable (the validator rejects null too, so an unsatisfiable field must not advertise null as valid) and then contradicts the value space via applyEmptyValueSpace. */
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
        }
    }
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

/* @important tracks whether the scan is inside a regex character class [...] so the bracket/comma bookkeeping treats ')', ']', '}', '(', '{' and ',' as literal class members. A ']' is a literal (not a close) when it is the class's first content character — and the leading negation '^' does not count as content — mirroring regexp/syntax. */
type charClassScanner struct {
    inClass      bool
    contentSeen  bool
    caretAllowed bool
}

func (instance *charClassScanner) step(character rune) bool {
    if true == instance.inClass {
        if ('^' == character) && (false == instance.contentSeen) && (true == instance.caretAllowed) {
            instance.caretAllowed = false

            return true
        }

        instance.caretAllowed = false

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

        return true
    }

    return false
}

func (instance *charClassScanner) noteEscaped() {
    if true == instance.inClass {
        instance.caretAllowed = false
        instance.contentSeen = true
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
        case ']':
            return false
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
