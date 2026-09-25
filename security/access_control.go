package security

import (
    stdpath "path"
    "regexp"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func normalizeAccessControlAttributes(attributes []string) []string {
    normalizedAttributes := make([]string, 0, len(attributes))
    publicAccess := false
    for _, attribute := range attributes {
        normalizedAttribute := strings.TrimSpace(attribute)
        if "" == normalizedAttribute {
            continue
        }

        if securitycontract.AttributePublicAccess == normalizedAttribute {
            publicAccess = true
        }

        normalizedAttributes = append(normalizedAttributes, normalizedAttribute)
    }

    if true == publicAccess && 1 < len(normalizedAttributes) {
        exception.Panic(
            exception.NewError("access control PUBLIC_ACCESS may not be combined with other attributes", nil, nil),
        )
    }

    /* a blank attribute is refused: a rule whose attributes all normalize away would still match its path, grant every authenticated principal and shadow any longer-prefixed rule that denies */
    if 0 == len(normalizedAttributes) {
        exception.Panic(
            exception.NewError("access control rule requires at least one attribute", nil, nil),
        )
    }

    return normalizedAttributes
}

/* NewAccessControlRule builds a rule bounded to a path segment: "/admin" governs "/admin" and "/admin/panel" but not "/administrator". An empty prefix is refused rather than made a catch-all, and PUBLIC_ACCESS is allowed, since a segment-bounded public rule cannot shadow a bounded denial; NewAccessControlRawPrefixRule is the cross-segment exception. */
func NewAccessControlRule(pathPrefix string, attributes ...string) AccessControlRule {
    normalizedPrefix := normalizePathPrefix(pathPrefix)

    /* an empty prefix is refused, as the exact and regex constructors refuse empty input: it would normalize to "" and become a catch-all fallback; a global rule declares "/" */
    if "" == normalizedPrefix {
        exception.Panic(
            exception.NewError("access control segment prefix may not be empty", nil, nil),
        )
    }

    if "/" != normalizedPrefix && true == strings.HasSuffix(normalizedPrefix, "/") {
        normalizedPrefix = strings.TrimSuffix(normalizedPrefix, "/")
    }

    normalizedAttributes := normalizeAccessControlAttributes(attributes)

    return AccessControlRule{
        pathPrefix:      normalizedPrefix,
        attributes:      normalizedAttributes,
        isExact:         false,
        isRegex:         false,
        isSegmentPrefix: true,
    }
}

/* NewAccessControlRawPrefixRule builds a rule that matches every path beginning with pathPrefix, so "/admin" governs "/administrator" as readily as "/admin/panel". Being the longest match, a raw rule shadows a bounded rule that would deny, so PUBLIC_ACCESS is refused on it. Use NewAccessControlRule unless a cross-segment reach is what the rule means. */
func NewAccessControlRawPrefixRule(pathPrefix string, attributes ...string) AccessControlRule {
    for _, attribute := range attributes {
        if securitycontract.AttributePublicAccess == strings.TrimSpace(attribute) {
            exception.Panic(
                exception.NewError("access control PUBLIC_ACCESS may not be declared on a raw prefix rule; use a segment prefix, exact, or regex rule", nil, nil),
            )
        }
    }

    return newAccessControlPrefixRule(pathPrefix, attributes)
}

func newAccessControlPrefixRule(pathPrefix string, attributes []string) AccessControlRule {
    normalizedPrefix := normalizePathPrefix(pathPrefix)

    normalizedAttributes := normalizeAccessControlAttributes(attributes)

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

    rule := newAccessControlPrefixRule("", attributes)
    rule.pathPrefix = normalizedPath
    rule.isExact = true

    return rule
}

/* accessControlRegexPatternIsAnchored reports whether a pattern is bound to the path start ("^"), which keeps a public rule out of the middle of an unrelated path. A start-anchored pattern can still over-match at its tail, but that only shadows a route guarded by a later regex, since exact and prefix rules outrank every regex. */
func accessControlRegexPatternIsAnchored(pattern string) bool {
    return strings.HasPrefix(pattern, "^")
}

/* NewAccessControlRegexRule builds a rule that matches when the pattern is found anywhere in the canonicalized request path: it is compiled unanchored, so "/public" matches "/admin/public-notes", unlike a route requirement, which melody anchors. Write "^/public(/|$)" to bound it to one tree. Regex rules match after exact and prefix rules, and among themselves the first registered wins. */
func NewAccessControlRegexRule(pattern string, attributes ...string) AccessControlRule {
    normalizedPattern := strings.TrimSpace(pattern)
    if "" == normalizedPattern {
        exception.Panic(
            exception.NewError("access control regex pattern may not be empty", nil, nil),
        )
    }

    /* PUBLIC_ACCESS on a pattern not anchored to the path start would open every path it matches as a substring, and the first registered regex wins, so it is refused, as on a raw prefix rule; a start-anchored pattern such as "^/public(/|$)" stays allowed */
    for _, attribute := range attributes {
        if securitycontract.AttributePublicAccess == strings.TrimSpace(attribute) && false == accessControlRegexPatternIsAnchored(normalizedPattern) {
            exception.Panic(
                exception.NewError("access control PUBLIC_ACCESS may not be declared on an unanchored regex rule; anchor the pattern to the path start with ^ (for example ^/public(/|$)) so it cannot match inside a protected path", nil, nil),
            )
        }
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

    rule := newAccessControlPrefixRule("", attributes)
    rule.regexPattern = normalizedPattern
    rule.regexCompiled = compiled
    rule.isRegex = true

    return rule
}

/* Deprecated: use NewAccessControlRule, which now builds the segment-prefix rule this constructor always built. Kept as an alias so existing callers continue to compile. */
func NewAccessControlRuleWithSegmentPrefix(pathPrefix string, attributes ...string) AccessControlRule {
    return NewAccessControlRule(pathPrefix, attributes...)
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
                NewAccessControlRule(rule.pathPrefix, rule.attributes...),
            )
            continue
        }

        normalizedRules = append(
            normalizedRules,
            NewAccessControlRawPrefixRule(rule.pathPrefix, rule.attributes...),
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

/* Match resolves by category before position: an exact rule beats every prefix rule, a longer prefix beats a shorter one, every prefix beats every regex, and the empty-prefix fallback answers last. Registration order breaks only the ties inside a category. */
func (instance *AccessControl) Match(path string) ([]string, bool) {
    matchedIndex, matched := instance.matchRuleIndex(path)
    if false == matched {
        return []string{}, false
    }

    return append([]string{}, instance.rules[matchedIndex].attributes...), true
}

/* canonicalizeAccessControlPath folds the spellings that reach one resource, "//admin" or "/open/../admin", into the one the rules are written in, and trims surrounding whitespace. Neither is a defence on its own, since the router serves the sent spelling: the http kernel refuses a non-canonical path before routing or authorization (http.requestPathIsCanonical), so every path it hands here is already canonical. The fold and the trim serve a caller that consults AccessControl without that guard. */
func canonicalizeAccessControlPath(requestPath string) string {
    canonicalPath := strings.TrimSpace(requestPath)
    if "" == canonicalPath {
        return "/"
    }

    if false == strings.HasPrefix(canonicalPath, "/") {
        canonicalPath = "/" + canonicalPath
    }

    canonicalPath = stdpath.Clean(canonicalPath)
    if "." == canonicalPath || "" == canonicalPath {
        return "/"
    }

    return canonicalPath
}

func (instance *AccessControl) matchRuleIndex(path string) (int, bool) {
    normalizedPath := canonicalizeAccessControlPath(path)

    for index, rule := range instance.rules {
        if true == rule.isExact {
            if normalizedPath == rule.pathPrefix {
                return index, true
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
        return bestIndex, true
    }

    for index, rule := range instance.rules {
        if false == rule.isRegex {
            continue
        }

        if nil == rule.regexCompiled {
            continue
        }

        if true == rule.regexCompiled.MatchString(normalizedPath) {
            return index, true
        }
    }

    if -1 != fallbackIndex {
        return fallbackIndex, true
    }

    return -1, false
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
