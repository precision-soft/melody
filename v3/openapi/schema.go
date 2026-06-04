package openapi

import (
    "reflect"
    "strconv"
    "strings"
    "time"
)

var timeType = reflect.TypeOf(time.Time{})

func schemaFromType(targetType reflect.Type) *Schema {
    return buildSchema(targetType, make(map[reflect.Type]bool))
}

func buildSchema(targetType reflect.Type, visited map[reflect.Type]bool) *Schema {
    if nil == targetType {
        return &Schema{}
    }

    for reflect.Ptr == targetType.Kind() {
        targetType = targetType.Elem()
    }

    if targetType == timeType {
        return &Schema{Type: "string", Format: "date-time"}
    }

    switch targetType.Kind() {
    case reflect.String:
        return &Schema{Type: "string"}
    case reflect.Bool:
        return &Schema{Type: "boolean"}
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
        reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        return &Schema{Type: "integer"}
    case reflect.Float32, reflect.Float64:
        return &Schema{Type: "number"}
    case reflect.Slice, reflect.Array:
        return &Schema{Type: "array", Items: buildSchema(targetType.Elem(), visited)}
    case reflect.Map:
        return &Schema{Type: "object", AdditionalProperties: buildSchema(targetType.Elem(), visited)}
    case reflect.Struct:
        return buildStructSchema(targetType, visited)
    default:
        return &Schema{}
    }
}

func buildStructSchema(structType reflect.Type, visited map[reflect.Type]bool) *Schema {
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

    for index := 0; index < structType.NumField(); index++ {
        field := structType.Field(index)
        if false == field.IsExported() {
            continue
        }

        jsonName, omit := jsonFieldName(field)
        if true == omit {
            continue
        }

        propertySchema := buildSchema(field.Type, visited)
        applyValidation(propertySchema, field.Tag.Get("validate"))

        schema.Properties[jsonName] = propertySchema

        if true == isRequired(field.Tag.Get("validate")) {
            required = append(required, jsonName)
        }
    }

    if 0 == len(schema.Properties) {
        schema.Properties = nil
    }

    if 0 < len(required) {
        schema.Required = required
    }

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
    for _, rule := range splitRules(validateTag) {
        name, param := splitRule(rule)

        switch name {
        case "email":
            schema.Format = "email"
        case "min":
            if value, parseErr := strconv.Atoi(param); nil == parseErr {
                schema.MinLength = &value
            }
        case "max":
            if value, parseErr := strconv.Atoi(param); nil == parseErr {
                schema.MaxLength = &value
            }
        case "regex":
            schema.Pattern = param
        case "greaterThan":
            if value, parseErr := strconv.ParseFloat(param, 64); nil == parseErr {
                schema.Minimum = &value
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
