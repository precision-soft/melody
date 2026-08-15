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

    /* a rule whose attributes all normalize away still matches its path, and an empty attribute list grants every authenticated principal while shadowing any longer-prefixed rule that would have denied; the blank attribute is refused here rather than degrading the guard silently */
    if 0 == len(normalizedAttributes) {
        exception.Panic(
            exception.NewError("access control rule requires at least one attribute", nil, nil),
        )
    }

    return normalizedAttributes
}

func NewAccessControlRule(pathPrefix string, attributes ...string) AccessControlRule {
    /* a raw prefix rule matches across segment boundaries, so PUBLIC_ACCESS on it opens every path that merely begins with the prefix and, being the longest match, shadows a correctly bounded rule that would have denied. A public path is declared through a segment prefix (AllowAnonymous), an exact rule, or a regex rule, none of which reach past their own boundary. */
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

/* NewAccessControlRegexRule builds a rule that matches when the pattern is found anywhere in the
canonicalized request path. The pattern is compiled UNANCHORED and tested with regexp.MatchString, so
it is a substring match, not a whole-path one: "/public" matches "/admin/public-notes" and
"/x/publications" as readily as "/public". This is deliberate and mirrors the path regex of other
frameworks, but it is the opposite of a route requirement, which melody anchors with ^(?:…)$ — so a
rule meant to name one section must anchor itself. Write "^/public(/|$)" to bound it to the /public
tree. Regex rules are the lowest match priority (after exact and prefix rules), and among themselves
the first registered that matches wins. */
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

    rule := newAccessControlPrefixRule("", attributes)
    rule.regexPattern = normalizedPattern
    rule.regexCompiled = compiled
    rule.isRegex = true

    return rule
}

func NewAccessControlRuleWithSegmentPrefix(pathPrefix string, attributes ...string) AccessControlRule {
    normalizedPrefix := normalizePathPrefix(pathPrefix)

    /* reject an empty segment prefix the way the exact and regex constructors reject empty input: an empty prefix would otherwise normalize to "" and become a catch-all fallback rule, so AllowAnonymous("") would silently open every unmatched path to anonymous access. A genuinely public service declares an explicit "/" prefix. */
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

/* Match resolves by category before position: an exact rule beats every prefix rule, a longer prefix beats a shorter one regardless of registration order, every prefix beats every regex, and the empty-prefix fallback answers only when nothing else did. Position in the rule list — what the merge strategies order — breaks only the ties inside a category: equal-length prefixes, regexes, exact duplicates and fallbacks each resolve to the first registered. */
func (instance *AccessControl) Match(path string) ([]string, bool) {
    matchedIndex, matched := instance.matchRuleIndex(path)
    if false == matched {
        return []string{}, false
    }

    return append([]string{}, instance.rules[matchedIndex].attributes...), true
}

/* canonicalizeAccessControlPath folds the spellings that reach the same resource into the one the rules are
written in. net/http hands the path through unfolded, so "//admin/panel" and "/open/../admin/panel" are
matched by no rule that names "/admin" — and no rule matched is granted, with the token never consulted.

Folding is NOT sufficient on its own, and the http kernel does not rely on it: because the router matches
the path as sent and does not fold "..", a request routed to a protected handler under a folded spelling
would be authorized here against the folded spelling's rule — a different, possibly more permissive one, or
none. The kernel closes that by refusing a non-canonical request path before it is routed or authorized
(http.requestPathIsCanonical), so every path this sees is already the one spelling. The fold remains as the
matcher's own defence for a caller that consults AccessControl without that guard, and it can only make a
rule match a request it did not match before — it never opens what a rule had closed for the same spelling.

The surrounding whitespace is trimmed before the fold rather than left alone: the trim makes the matcher
answer for one spelling more than the router accepts, which refuses more than intended and never less. */
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
