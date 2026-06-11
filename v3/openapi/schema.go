package openapi

import (
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

    /** @info embedCount tracks how many equal-depth paths reach an embedded type, mirroring encoding/json: a type reached via N paths has its fields duplicated N times so a diamond annihilates in dominantEmbeddedField. */
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

    if true == isRequired(field.Tag.Get("validate")) {
        *required = append(*required, jsonName)
    }
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

func applyValidation(schema *Schema, validateTag string) {
    if "" != schema.Ref || nil != schema.AllOf {
        return
    }

    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)

        switch name {
        case "email":
            schema.Format = "email"
        case "min":
            if "string" == schema.Type {
                if valueString, exists := params["value"]; true == exists {
                    if value, parseErr := strconv.Atoi(valueString); nil == parseErr {
                        schema.MinLength = &value
                    }
                } else {
                    /** @important a value-less min constraint is enforced as minLength 1 by the validator, so the spec must advertise the same bound */
                    defaultMinLength := 1
                    schema.MinLength = &defaultMinLength
                }
            }
        case "max":
            if "string" == schema.Type {
                if valueString, exists := params["value"]; true == exists {
                    if value, parseErr := strconv.Atoi(valueString); nil == parseErr {
                        schema.MaxLength = &value
                    }
                } else {
                    /** @important a value-less max constraint is enforced as maxLength 100 by the validator, so the spec must advertise the same bound */
                    defaultMaxLength := 100
                    schema.MaxLength = &defaultMaxLength
                }
            }
        case "regex", "pattern":
            if "string" == schema.Type {
                schema.Pattern = patternParam(params)
            }
        case "greaterThan":
            if "integer" == schema.Type || "number" == schema.Type {
                exclusive := true
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parseErr := strconv.ParseFloat(valueString, 64); nil == parseErr {
                        value := float64(int64(parsed))
                        schema.Minimum = &value
                        schema.ExclusiveMinimum = &exclusive
                    }
                } else {
                    /** @important a value-less greaterThan constraint is enforced as > 0 by the validator, so the spec must advertise the same bound */
                    value := float64(0)
                    schema.Minimum = &value
                    schema.ExclusiveMinimum = &exclusive
                }
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {
                exclusive := true
                if valueString, exists := params["value"]; true == exists {
                    if parsed, parseErr := strconv.ParseFloat(valueString, 64); nil == parseErr {
                        value := float64(int64(parsed))
                        schema.Maximum = &value
                        schema.ExclusiveMaximum = &exclusive
                    }
                } else {
                    /** @important a value-less lessThan constraint is enforced as < 0 by the validator, so the spec must advertise the same bound */
                    value := float64(0)
                    schema.Maximum = &value
                    schema.ExclusiveMaximum = &exclusive
                }
            }
        }
    }
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

func splitTopLevelRules(input string) []string {
    var parts []string

    bracketsBalanced := hasBalancedRuleBrackets(input)

    current := strings.Builder{}
    parenDepth := 0
    squareDepth := 0
    curlyDepth := 0
    wasEscaped := false

    for _, character := range input {
        if true == wasEscaped {
            current.WriteRune(character)
            wasEscaped = false
            continue
        }

        if '\\' == character {
            current.WriteRune(character)
            wasEscaped = true
            continue
        }

        if true == bracketsBalanced {
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

            if '[' == character {
                squareDepth++
                current.WriteRune(character)
                continue
            }

            if ']' == character {
                if 0 < squareDepth {
                    squareDepth--
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
            if 0 == parenDepth && 0 == squareDepth && 0 == curlyDepth {
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
    squareDepth := 0
    curlyDepth := 0
    isInSingleQuote := false
    isInDoubleQuote := false
    wasEscaped := false

    for _, character := range input {
        if true == wasEscaped {
            current.WriteRune(character)
            wasEscaped = false
            continue
        }

        if '\\' == character {
            current.WriteRune(character)
            wasEscaped = true
            continue
        }

        if '"' == character {
            if false == isInSingleQuote {
                isInDoubleQuote = false == isInDoubleQuote
            }
            current.WriteRune(character)
            continue
        }

        if '\'' == character {
            if false == isInDoubleQuote {
                isInSingleQuote = false == isInSingleQuote
            }
            current.WriteRune(character)
            continue
        }

        if false == isInSingleQuote && false == isInDoubleQuote {
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

            if '[' == character {
                squareDepth++
                current.WriteRune(character)
                continue
            }

            if ']' == character {
                if 0 < squareDepth {
                    squareDepth--
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
                if 0 == parenDepth && 0 == squareDepth && 0 == curlyDepth {
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
    squareDepth := 0
    curlyDepth := 0
    wasEscaped := false

    for _, character := range input {
        if true == wasEscaped {
            wasEscaped = false
            continue
        }

        if '\\' == character {
            wasEscaped = true
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
        case '[':
            squareDepth++
        case ']':
            if 0 == squareDepth {
                return false
            }
            squareDepth--
        case '{':
            curlyDepth++
        case '}':
            if 0 == curlyDepth {
                return false
            }
            curlyDepth--
        }
    }

    return 0 == parenDepth && 0 == squareDepth && 0 == curlyDepth
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
