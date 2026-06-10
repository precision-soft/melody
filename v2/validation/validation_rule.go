package validation

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

type validationRule struct {
    name   string
    params map[string]string
}

func splitByTopLevelComma(valueString string) []string {
    var parts []string

    current := strings.Builder{}
    parenDepth := 0
    wasEscaped := false

    for _, character := range valueString {
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

        if ',' == character {
            if 0 == parenDepth {
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

func hasBalancedBrackets(valueString string) bool {
    parenDepth := 0
    squareDepth := 0
    curlyDepth := 0
    wasEscaped := false

    for _, character := range valueString {
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

func splitByCommaOutsideRegexMeta(valueString string) []string {
    var parts []string

    current := strings.Builder{}
    parenDepth := 0
    squareDepth := 0
    curlyDepth := 0
    isInSingleQuote := false
    isInDoubleQuote := false
    wasEscaped := false

    for _, character := range valueString {
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

            if ',' == character {
                if 0 == squareDepth && 0 == curlyDepth && 0 == parenDepth {
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

func parseInt(valueString string, defaultValue int) int {
    var result int
    _, err := fmt.Sscanf(valueString, "%d", &result)
    if nil != err {
        return defaultValue
    }

    return result
}

func parseValidationTag(tag string) ([]validationRule, error) {
    var rules []validationRule

    parts := splitByTopLevelComma(tag)
    for _, rawPart := range parts {
        part := strings.TrimSpace(rawPart)
        if "" == part {
            continue
        }

        rule := validationRule{
            params: make(map[string]string),
        }

        openIndex := strings.Index(part, "(")
        equalIndex := strings.Index(part, "=")

        isParenthesized := 0 <= openIndex && (0 > equalIndex || openIndex < equalIndex)

        if true == isParenthesized {
            lastIndex := len(part) - 1
            if ')' != part[lastIndex] {
                return nil, exception.NewError(
                    "invalid validation tag syntax",
                    exceptioncontract.Context{
                        "tag":  tag,
                        "part": part,
                    },
                    nil,
                )
            }

            name := strings.TrimSpace(part[:openIndex])
            if "" == name {
                return nil, exception.NewError(
                    "invalid validation tag syntax",
                    exceptioncontract.Context{
                        "tag":  tag,
                        "part": part,
                    },
                    nil,
                )
            }

            paramsString := strings.TrimSpace(part[openIndex+1 : lastIndex])

            if false == hasBalancedBrackets(paramsString) {
                return nil, exception.NewError(
                    "invalid validation tag syntax",
                    exceptioncontract.Context{
                        "tag":  tag,
                        "part": part,
                    },
                    nil,
                )
            }

            rule.name = name

            if "" != paramsString {
                paramPairs := splitByCommaOutsideRegexMeta(paramsString)
                for _, pair := range paramPairs {
                    pair = strings.TrimSpace(pair)
                    if "" == pair {
                        continue
                    }

                    keyValue := strings.SplitN(pair, "=", 2)
                    if 2 != len(keyValue) {
                        return nil, exception.NewError(
                            "invalid validation tag syntax",
                            exceptioncontract.Context{
                                "tag":  tag,
                                "part": part,
                            },
                            nil,
                        )
                    }

                    key := strings.TrimSpace(keyValue[0])
                    value := strings.TrimSpace(keyValue[1])

                    if "" == key {
                        return nil, exception.NewError(
                            "invalid validation tag syntax",
                            exceptioncontract.Context{
                                "tag":  tag,
                                "part": part,
                            },
                            nil,
                        )
                    }

                    rule.params[key] = value
                }
            }

            rules = append(rules, rule)
            continue
        }

        if strings.Contains(part, "=") {
            keyValue := strings.SplitN(part, "=", 2)
            if 2 != len(keyValue) {
                return nil, exception.NewError(
                    "invalid validation tag syntax",
                    exceptioncontract.Context{
                        "tag":  tag,
                        "part": part,
                    },
                    nil,
                )
            }

            rule.name = strings.TrimSpace(keyValue[0])
            if "" == rule.name {
                return nil, exception.NewError(
                    "invalid validation tag syntax",
                    exceptioncontract.Context{
                        "tag":  tag,
                        "part": part,
                    },
                    nil,
                )
            }

            rule.params["value"] = strings.TrimSpace(keyValue[1])

            rules = append(rules, rule)
            continue
        }

        rule.name = strings.TrimSpace(part)
        if "" == rule.name {
            continue
        }

        rules = append(rules, rule)
    }

    return rules, nil
}
