package translation

import (
    "encoding/json"
    "fmt"
    "math"
    "strconv"
    "strings"
)

const maxInterpolationDepth = 32

func formatMessage(pattern string, parameters map[string]any, locale string) string {
    return interpolate([]rune(pattern), parameters, locale, "", false, 0)
}

func interpolate(runes []rune, parameters map[string]any, locale string, pound string, inPlural bool, depth int) string {
    if depth > maxInterpolationDepth {
        return string(runes)
    }

    var builder strings.Builder

    index := 0
    for index < len(runes) {
        current := runes[index]

        if '#' == current && true == inPlural {
            builder.WriteString(pound)
            index++
            continue
        }

        if '{' != current {
            builder.WriteRune(current)
            index++
            continue
        }

        closeIndex := matchingBrace(runes, index)
        if -1 == closeIndex {
            builder.WriteRune(current)
            index++
            continue
        }

        builder.WriteString(evaluateArgument(runes[index+1:closeIndex], parameters, locale, pound, inPlural, depth))
        index = closeIndex + 1
    }

    return builder.String()
}

func matchingBrace(runes []rune, openIndex int) int {
    depth := 0
    for index := openIndex; index < len(runes); index++ {
        if '{' == runes[index] {
            depth++
        } else if '}' == runes[index] {
            depth--
            if 0 == depth {
                return index
            }
        }
    }

    return -1
}

func evaluateArgument(inner []rune, parameters map[string]any, locale string, pound string, inPlural bool, depth int) string {
    name, remainder, hasType := splitFirstComma(inner)
    trimmedName := strings.TrimSpace(string(name))

    if false == hasType {
        return stringifyParameter(parameters[trimmedName])
    }

    argType, style, hasStyle := splitFirstComma(remainder)
    trimmedType := strings.TrimSpace(string(argType))

    if false == hasStyle {
        return stringifyParameter(parameters[trimmedName])
    }

    switch trimmedType {
    case "plural":
        return evaluatePlural(trimmedName, style, parameters, locale, depth)
    case "select":
        return evaluateSelect(trimmedName, style, parameters, locale, pound, inPlural, depth)
    default:
        return stringifyParameter(parameters[trimmedName])
    }
}

func evaluateSelect(name string, style []rune, parameters map[string]any, locale string, pound string, inPlural bool, depth int) string {
    selectors := parseSelectors(style)

    keyword := stringifyParameter(parameters[name])
    block, found := selectors[keyword]
    if false == found {
        block, found = selectors["other"]
    }

    if false == found {
        return ""
    }

    return interpolate(block, parameters, locale, pound, inPlural, depth+1)
}

func evaluatePlural(name string, style []rune, parameters map[string]any, locale string, depth int) string {
    selectors := parseSelectors(style)

    number, hasNumber := toFloat(parameters[name])
    if false == hasNumber {
        if block, found := selectors["other"]; true == found {
            return interpolate(block, parameters, locale, "", true, depth+1)
        }

        return ""
    }

    pound := formatPoundValue(parameters[name], number)

    if block, found := selectors["="+pound]; true == found {
        return interpolate(block, parameters, locale, pound, true, depth+1)
    }

    category := pluralCategory(locale, number)
    if block, found := selectors[category]; true == found {
        return interpolate(block, parameters, locale, pound, true, depth+1)
    }

    if block, found := selectors["other"]; true == found {
        return interpolate(block, parameters, locale, pound, true, depth+1)
    }

    return ""
}

func parseSelectors(style []rune) map[string][]rune {
    selectors := make(map[string][]rune)

    index := 0
    for index < len(style) {
        for index < len(style) && true == isSpace(style[index]) {
            index++
        }

        if index >= len(style) {
            break
        }

        keywordStart := index
        for index < len(style) && false == isSpace(style[index]) && '{' != style[index] {
            index++
        }
        keyword := string(style[keywordStart:index])

        for index < len(style) && true == isSpace(style[index]) {
            index++
        }

        if index >= len(style) || '{' != style[index] {
            break
        }

        closeIndex := matchingBrace(style, index)
        if -1 == closeIndex {
            break
        }

        if "" != keyword {
            selectors[keyword] = style[index+1 : closeIndex]
        }

        index = closeIndex + 1
    }

    return selectors
}

func splitFirstComma(value []rune) ([]rune, []rune, bool) {
    for index := 0; index < len(value); index++ {
        if ',' == value[index] {
            return value[:index], value[index+1:], true
        }
    }

    return value, nil, false
}

func isSpace(value rune) bool {
    return ' ' == value || '\t' == value || '\n' == value || '\r' == value
}

func stringifyParameter(value any) string {
    if nil == value {
        return ""
    }

    switch typed := value.(type) {
    case string:
        return typed
    case bool:
        return strconv.FormatBool(typed)
    case int:
        return strconv.Itoa(typed)
    case int64:
        return strconv.FormatInt(typed, 10)
    case float64:
        return formatNumber(typed)
    default:
        return fmt.Sprintf("%v", typed)
    }
}

func formatPoundValue(raw any, number float64) string {
    switch typed := raw.(type) {
    case int:
        return strconv.FormatInt(int64(typed), 10)
    case int8:
        return strconv.FormatInt(int64(typed), 10)
    case int16:
        return strconv.FormatInt(int64(typed), 10)
    case int32:
        return strconv.FormatInt(int64(typed), 10)
    case int64:
        return strconv.FormatInt(typed, 10)
    case uint:
        return strconv.FormatUint(uint64(typed), 10)
    case uint8:
        return strconv.FormatUint(uint64(typed), 10)
    case uint16:
        return strconv.FormatUint(uint64(typed), 10)
    case uint32:
        return strconv.FormatUint(uint64(typed), 10)
    case uint64:
        return strconv.FormatUint(typed, 10)
    case json.Number:
        return typed.String()
    case float32:
        return strconv.FormatFloat(float64(typed), 'f', -1, 32)
    default:
        return formatNumber(number)
    }
}

func toFloat(value any) (float64, bool) {
    switch typed := value.(type) {
    case int:
        return float64(typed), true
    case int8:
        return float64(typed), true
    case int16:
        return float64(typed), true
    case int32:
        return float64(typed), true
    case int64:
        return float64(typed), true
    case uint:
        return float64(typed), true
    case uint8:
        return float64(typed), true
    case uint16:
        return float64(typed), true
    case uint32:
        return float64(typed), true
    case uint64:
        return float64(typed), true
    case float32:
        return float64(typed), true
    case float64:
        return typed, true
    case json.Number:
        parsed, parseErr := typed.Float64()
        if nil != parseErr {
            return 0, false
        }
        return parsed, true
    case string:
        parsed, parseErr := strconv.ParseFloat(typed, 64)
        if nil != parseErr {
            return 0, false
        }
        return parsed, true
    default:
        return 0, false
    }
}

const maxExactInteger = 9007199254740992.0

func formatNumber(value float64) string {
    if true == math.IsNaN(value) || true == math.IsInf(value, 0) {
        return strconv.FormatFloat(value, 'f', -1, 64)
    }

    if value == math.Trunc(value) && value >= -maxExactInteger && value <= maxExactInteger {
        return strconv.FormatInt(int64(value), 10)
    }

    return strconv.FormatFloat(value, 'f', -1, 64)
}
