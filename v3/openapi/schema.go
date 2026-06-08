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

    ownCandidatesByName := make(map[string][]embeddedCandidate)
    var ownOrder []string
    var embedQueue []reflect.Type
    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if true == isPromotedEmbed(field) {
            embedQueue = append(embedQueue, dereferencedStructType(field.Type))
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

        for _, embeddedType := range embedQueue {
            for index := 0; index < embeddedType.NumField(); index++ {
                field := embeddedType.Field(index)

                if true == isPromotedEmbed(field) {
                    nextLevel = append(nextLevel, dereferencedStructType(field.Type))
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
                candidatesByName[jsonName] = append(candidatesByName[jsonName], embeddedCandidate{field: field})
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

    if "" != field.Tag.Get("json") {
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
    if "" != schema.Ref {
        return
    }

    for _, rule := range splitRules(validateTag) {
        name, params := splitRule(rule)

        switch name {
        case "email":
            schema.Format = "email"
        case "min":
            if value, parseErr := strconv.Atoi(params["value"]); nil == parseErr {
                if "string" == schema.Type {
                    schema.MinLength = &value
                }
            }
        case "max":
            if value, parseErr := strconv.Atoi(params["value"]); nil == parseErr {
                if "string" == schema.Type {
                    schema.MaxLength = &value
                }
            }
        case "regex", "pattern":
            if "string" == schema.Type {
                schema.Pattern = patternParam(params)
            }
        case "greaterThan":
            if "integer" == schema.Type || "number" == schema.Type {
                if value, parseErr := strconv.ParseFloat(params["value"], 64); nil == parseErr {
                    exclusive := true
                    schema.Minimum = &value
                    schema.ExclusiveMinimum = &exclusive
                }
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {
                if value, parseErr := strconv.ParseFloat(params["value"], 64); nil == parseErr {
                    exclusive := true
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

    return splitByCommaOutsideGroups(trimmed)
}

func splitByCommaOutsideGroups(input string) []string {
    var parts []string
    depth := 0
    start := 0
    for index := 0; index < len(input); index++ {
        switch input[index] {
        case '(', '{', '[':
            depth++
        case ')', '}', ']':
            if 0 < depth {
                depth--
            }
        case ',':
            if 0 == depth {
                parts = append(parts, input[start:index])
                start = index + 1
            }
        }
    }
    parts = append(parts, input[start:])

    return parts
}

func splitRule(rule string) (string, map[string]string) {
    trimmed := strings.TrimSpace(rule)
    params := make(map[string]string)

    openIndex := strings.IndexByte(trimmed, '(')
    if -1 != openIndex && true == strings.HasSuffix(trimmed, ")") {
        name := strings.TrimSpace(trimmed[:openIndex])
        inner := trimmed[openIndex+1 : len(trimmed)-1]
        for _, pair := range splitByCommaOutsideGroups(inner) {
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

    separator := strings.IndexByte(trimmed, '=')
    if -1 == separator {
        return trimmed, params
    }

    params["value"] = strings.TrimSpace(trimmed[separator+1:])

    return strings.TrimSpace(trimmed[:separator]), params
}
