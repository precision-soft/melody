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

func collectStructFields(
    structType reflect.Type,
    components map[string]*Schema,
    names map[reflect.Type]string,
    visited map[reflect.Type]bool,
    properties map[string]*Schema,
    required *[]string,
) {
    // The struct's own fields are collected before any promoted embed so that an outer
    // field shadows a same-named embedded field regardless of declaration order, matching
    // encoding/json (shallower fields win). The first-wins skip in the embed pass then drops
    // the shadowed embedded field.
    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if true == isPromotedEmbed(field) {
            continue
        }

        if false == field.IsExported() {
            continue
        }

        jsonName, omit := jsonFieldName(field)
        if true == omit {
            continue
        }

        if _, exists := properties[jsonName]; true == exists {
            continue
        }

        propertySchema := buildSchema(field.Type, components, names, visited)
        applyValidation(propertySchema, field.Tag.Get("validate"))
        properties[jsonName] = propertySchema

        if true == isRequired(field.Tag.Get("validate")) {
            *required = append(*required, jsonName)
        }
    }

    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)

        if false == isPromotedEmbed(field) {
            continue
        }

        embedded := field.Type
        for reflect.Ptr == embedded.Kind() {
            embedded = embedded.Elem()
        }

        collectStructFields(embedded, components, names, visited, properties, required)
    }
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
            schema.Pattern = param
        case "greaterThan":
            if value, parseErr := strconv.ParseFloat(param, 64); nil == parseErr {
                exclusive := true
                schema.Minimum = &value
                schema.ExclusiveMinimum = &exclusive
            }
        case "lessThan":
            if value, parseErr := strconv.ParseFloat(param, 64); nil == parseErr {
                exclusive := true
                schema.Maximum = &value
                schema.ExclusiveMaximum = &exclusive
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
