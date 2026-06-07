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
        /** encoding/json — which the typed handler and Request.BindJson both use — serializes a []byte
            slice as a base64-encoded string, not an array of integers; describe it as such so the generated
            spec matches the wire format. Fixed byte arrays ([N]byte) are not special-cased by encoding/json
            and stay integer arrays. */
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
    /** resolved tracks json names already decided at a shallower depth — whether they were added or
        dropped as ambiguous — so a deeper embedded field can never override a shallower one. This
        mirrors encoding/json: the shallowest field with a given json name wins, and fields that tie
        at the minimum depth are dropped (unless exactly one of them is explicitly json-tagged). */
    resolved := make(map[string]bool)

    /** Depth 0: the struct's own (non-embedded) fields. A shallower field always wins, so they are
        claimed first regardless of an embedded field's declaration order. */
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

        if true == resolved[jsonName] {
            continue
        }

        resolved[jsonName] = true
        addFieldProperty(field, jsonName, components, names, visited, properties, required)
    }

    /** Promote embedded structs breadth-first by depth: every embed at depth N is resolved before
        any embed at depth N+1, so the shallowest json name wins and equal-depth ties are dropped. */
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

    /** More than one promoted field ties at this depth for the same json name: encoding/json keeps
        the single explicitly-tagged one if there is exactly one, and otherwise drops them all. */
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
    if "-" == parts[0] {
        return "", true
    }

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
        name, param := splitRule(rule)

        switch name {
        case "email":
            schema.Format = "email"
        case "min":
            if value, parseErr := strconv.Atoi(param); nil == parseErr {
                if "string" == schema.Type {
                    schema.MinLength = &value
                }
            }
        case "max":
            if value, parseErr := strconv.Atoi(param); nil == parseErr {
                if "string" == schema.Type {
                    schema.MaxLength = &value
                }
            }
        case "regex", "pattern":
            if "string" == schema.Type {
                schema.Pattern = param
            }
        case "greaterThan":
            if "integer" == schema.Type || "number" == schema.Type {
                if value, parseErr := strconv.ParseFloat(param, 64); nil == parseErr {
                    exclusive := true
                    schema.Minimum = &value
                    schema.ExclusiveMinimum = &exclusive
                }
            }
        case "lessThan":
            if "integer" == schema.Type || "number" == schema.Type {
                if value, parseErr := strconv.ParseFloat(param, 64); nil == parseErr {
                    exclusive := true
                    schema.Maximum = &value
                    schema.ExclusiveMaximum = &exclusive
                }
            }
        }
    }
}

func splitRules(validateTag string) []string {
    trimmed := strings.TrimSpace(validateTag)
    if "" == trimmed || "-" == trimmed {
        return nil
    }

    return strings.Split(trimmed, ",")
}

func splitRule(rule string) (string, string) {
    trimmed := strings.TrimSpace(rule)

    separator := strings.IndexByte(trimmed, '=')
    if -1 == separator {
        return trimmed, ""
    }

    return strings.TrimSpace(trimmed[:separator]), strings.TrimSpace(trimmed[separator+1:])
}
