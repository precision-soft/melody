package security

import (
    "regexp"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

func NewAccessControlRule(pathPrefix string, attributes ...string) AccessControlRule {
    normalizedPrefix := normalizePathPrefix(pathPrefix)

    normalizedAttributes := make([]string, 0, len(attributes))
    for _, attribute := range attributes {
        normalizedAttribute := strings.TrimSpace(attribute)
        if "" == normalizedAttribute {
            continue
        }

        normalizedAttributes = append(normalizedAttributes, normalizedAttribute)
    }

    return AccessControlRule{
        pathPrefix:      normalizedPrefix,
        attributes:      normalizedAttributes,
        isExact:         false,
        isRegex:         false,
        isSegmentPrefix: false,
    }
}

func NewAccessControlExactRule(path string, attributes ...string) AccessControlRule {
    normalizedPath := strings.TrimSpace(path)
    if "" == normalizedPath {
        exception.Panic(
            exception.NewError("access control exact path may not be empty", nil, nil),
        )
    }

    if "/" != normalizedPath {
        normalizedPath = strings.TrimSuffix(normalizedPath, "/")
    }

    rule := NewAccessControlRule("", attributes...)
    rule.pathPrefix = normalizedPath
    rule.isExact = true

    return rule
}

func NewAccessControlRegexRule(pattern string, attributes ...string) AccessControlRule {
    normalizedPattern := strings.TrimSpace(pattern)
    if "" == normalizedPattern {
        exception.Panic(
            exception.NewError("access control regex pattern may not be empty", nil, nil),
        )
    }

    compiled, compileErr := regexp.Compile(normalizedPattern)
    if nil != compileErr {
        exception.Panic(
            exception.NewError(
                "invalid access control regex pattern",
                exceptioncontract.Context{
                    "pattern": normalizedPattern,
                },
                compileErr,
            ),
        )
    }

    rule := NewAccessControlRule("", attributes...)
    rule.regexPattern = normalizedPattern
    rule.regexCompiled = compiled
    rule.isRegex = true

    return rule
}

func NewAccessControlRuleWithSegmentPrefix(pathPrefix string, attributes ...string) AccessControlRule {
    normalizedPrefix := normalizePathPrefix(pathPrefix)

    if "/" != normalizedPrefix && true == strings.HasSuffix(normalizedPrefix, "/") {
        normalizedPrefix = strings.TrimSuffix(normalizedPrefix, "/")
    }

    normalizedAttributes := make([]string, 0, len(attributes))
    for _, attribute := range attributes {
        normalizedAttribute := strings.TrimSpace(attribute)
        if "" == normalizedAttribute {
            continue
        }

        normalizedAttributes = append(normalizedAttributes, normalizedAttribute)
    }

    return AccessControlRule{
        pathPrefix:      normalizedPrefix,
        attributes:      normalizedAttributes,
        isExact:         false,
        isRegex:         false,
        isSegmentPrefix: true,
    }
}

type AccessControlRule struct {
    pathPrefix      string
    regexPattern    string
    regexCompiled   *regexp.Regexp
    attributes      []string
    isExact         bool
    isRegex         bool
    isSegmentPrefix bool
}

func NewAccessControl(rules ...AccessControlRule) *AccessControl {
    normalizedRules := make([]AccessControlRule, 0, len(rules))

    for _, rule := range rules {
        if true == rule.isRegex {
            normalizedRules = append(normalizedRules, rule)
            continue
        }

        if true == rule.isExact {
            normalizedRule := NewAccessControlExactRule(rule.pathPrefix, rule.attributes...)
            normalizedRules = append(normalizedRules, normalizedRule)
            continue
        }

        if true == rule.isSegmentPrefix {
            normalizedRules = append(
                normalizedRules,
                NewAccessControlRuleWithSegmentPrefix(rule.pathPrefix, rule.attributes...),
            )
            continue
        }

        normalizedRules = append(
            normalizedRules,
            NewAccessControlRule(rule.pathPrefix, rule.attributes...),
        )
    }

    return &AccessControl{
        rules: normalizedRules,
    }
}

type AccessControl struct {
    rules []AccessControlRule
}

func (instance *AccessControl) Rules() []AccessControlRule {
    return append([]AccessControlRule{}, instance.rules...)
}

func (instance *AccessControl) Match(path string) ([]string, bool) {
    normalizedPath := strings.TrimSpace(path)
    if "" == normalizedPath {
        normalizedPath = "/"
    }

    if "/" != normalizedPath {
        normalizedPath = strings.TrimSuffix(normalizedPath, "/")
    }

    for _, rule := range instance.rules {
        if true == rule.isExact {
            if normalizedPath == rule.pathPrefix {
                return append([]string{}, rule.attributes...), true
            }
        }
    }

    bestIndex := -1
    bestPrefixLength := -1

    fallbackIndex := -1

    for index, rule := range instance.rules {
        if true == rule.isRegex || true == rule.isExact {
            continue
        }

        if "" == rule.pathPrefix {
            if -1 == fallbackIndex {
                fallbackIndex = index
            }
            continue
        }

        isPrefixMatch := false

        if true == strings.HasPrefix(normalizedPath, rule.pathPrefix) {
            if false == rule.isSegmentPrefix {
                isPrefixMatch = true
            } else {
                if "/" == rule.pathPrefix {
                    isPrefixMatch = true
                } else {
                    prefixLength := len(rule.pathPrefix)

                    if len(normalizedPath) == prefixLength {
                        isPrefixMatch = true
                    } else {
                        if prefixLength < len(normalizedPath) && '/' == normalizedPath[prefixLength] {
                            isPrefixMatch = true
                        }
                    }
                }
            }
        }

        if true == isPrefixMatch {
            currentLength := len(rule.pathPrefix)

            if bestPrefixLength < currentLength {
                bestPrefixLength = currentLength
                bestIndex = index
            }

            continue
        }
    }

    if -1 != bestIndex {
        return append([]string{}, instance.rules[bestIndex].attributes...), true
    }

    for _, rule := range instance.rules {
        if false == rule.isRegex {
            continue
        }

        if nil == rule.regexCompiled {
            continue
        }

        if true == rule.regexCompiled.MatchString(normalizedPath) {
            return append([]string{}, rule.attributes...), true
        }
    }

    if -1 != fallbackIndex {
        return append([]string{}, instance.rules[fallbackIndex].attributes...), true
    }

    return []string{}, false
}

func normalizePathPrefix(pathPrefix string) string {
    normalizedPrefix := strings.TrimSpace(pathPrefix)
    if "" == normalizedPrefix {
        return ""
    }

    if "/" == normalizedPrefix {
        return "/"
    }

    normalizedPrefix = strings.TrimSuffix(normalizedPrefix, "/")

    return normalizedPrefix
}
