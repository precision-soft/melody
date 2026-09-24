package accesscontrol

import (
    "regexp"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* RuleConfig carries everything a rule declares beside its path and its matching mode. */
type RuleConfig struct {
    /* Attributes are the authorization attributes a matching request must satisfy: role names, or the single securitycontract.AttributePublicAccess. At least one is required, and PUBLIC_ACCESS may not be combined with any other. */
    Attributes []string
}

/* Rule is one access control rule: a path, the way that path is compared, and the attributes a matching request must satisfy. Build one with NewRule or with the mode-specific constructor beside it; the zero value governs nothing. */
type Rule struct {
    pathPrefix      string
    regexPattern    string
    regexCompiled   *regexp.Regexp
    attributes      []string
    isExact         bool
    isRegex         bool
    isSegmentPrefix bool
}

/* PathPrefix answers the normalized path the rule was declared with. For a regex rule it is empty and Pattern carries the declaration instead. */
func (instance Rule) PathPrefix() string {
    return instance.pathPrefix
}

/* Pattern answers the regex a MatchingRegex rule was declared with, and the empty string for every other mode. */
func (instance Rule) Pattern() string {
    return instance.regexPattern
}

/* Attributes answers a copy of the attributes a matching request must satisfy. */
func (instance Rule) Attributes() []string {
    return append([]string{}, instance.attributes...)
}

/* Matching answers the mode the rule compares its path with. */
func (instance Rule) Matching() Matching {
    if true == instance.isExact {
        return MatchingExact
    }

    if true == instance.isRegex {
        return MatchingRegex
    }

    if true == instance.isSegmentPrefix {
        return MatchingSegmentPrefix
    }

    return MatchingRawPrefix
}

/* NewRule builds a rule with the matching mode named at the call site. An unspecified mode is refused, since the reach is what an access control rule is for. */
func NewRule(path string, matching Matching, config RuleConfig) Rule {
    switch matching {
    case MatchingExact:
        return NewExactRule(path, config)
    case MatchingSegmentPrefix:
        return NewSegmentPrefixRule(path, config)
    case MatchingRawPrefix:
        return NewRawPrefixRule(path, config)
    case MatchingRegex:
        return NewRegexRule(path, config)
    }

    exception.Panic(
        exception.NewError(
            "access control rule requires a matching mode",
            map[string]any{
                "path":     path,
                "matching": matching.String(),
            },
            nil,
        ),
    )

    return Rule{}
}

/* NewExactRule builds a rule that governs one spelling and nothing beneath it: "/admin" claims "/admin" and refuses to speak for "/admin/panel". A trailing slash is folded away, so "/admin/" and "/admin" declare the same rule. */
func NewExactRule(path string, config RuleConfig) Rule {
    normalizedPath := strings.TrimSpace(path)
    if "" == normalizedPath {
        exception.Panic(
            exception.NewError("access control exact path may not be empty", nil, nil),
        )
    }

    if "/" != normalizedPath {
        normalizedPath = strings.TrimSuffix(normalizedPath, "/")
    }

    return Rule{
        pathPrefix: normalizedPath,
        attributes: normalizeAttributes(config.Attributes),
        isExact:    true,
    }
}

/* NewSegmentPrefixRule builds a rule bounded to a path segment: "/admin" governs "/admin" and "/admin/panel" but not "/administrator". An empty path is refused rather than made a catch-all; a global rule declares "/". */
func NewSegmentPrefixRule(path string, config RuleConfig) Rule {
    normalizedPrefix := normalizePathPrefix(path)
    if "" == normalizedPrefix {
        exception.Panic(
            exception.NewError("access control segment prefix may not be empty", nil, nil),
        )
    }

    return Rule{
        pathPrefix:      normalizedPrefix,
        attributes:      normalizeAttributes(config.Attributes),
        isSegmentPrefix: true,
    }
}

/* NewRawPrefixRule builds a rule that reaches across segment boundaries: "/admin" governs "/administrator" and "/admin-tools" as readily as "/admin/panel". PUBLIC_ACCESS is refused on it, since a raw public rule, being the longest match, would shadow a bounded denial. Reach for NewSegmentPrefixRule unless the cross-segment reach is what the rule means. */
func NewRawPrefixRule(path string, config RuleConfig) Rule {
    refusePublicAccess(config.Attributes, "a raw prefix rule; use a segment prefix, exact, or regex rule")

    return Rule{
        pathPrefix: normalizePathPrefix(path),
        attributes: normalizeAttributes(config.Attributes),
    }
}

/* NewRegexRule builds a rule that matches when the pattern is found anywhere in the canonicalized request path: it is compiled unanchored, so "/public" matches "/admin/public-notes"; write "^/public(/|$)" to bound it to the /public tree. Regex rules match after exact and prefix rules, and among themselves the first registered that matches wins. */
func NewRegexRule(pattern string, config RuleConfig) Rule {
    normalizedPattern := strings.TrimSpace(pattern)
    if "" == normalizedPattern {
        exception.Panic(
            exception.NewError("access control regex pattern may not be empty", nil, nil),
        )
    }

    refuseUnanchoredPublicAccess(normalizedPattern, config.Attributes)

    compiledPattern, compileErr := regexp.Compile(normalizedPattern)
    if nil != compileErr {
        exception.Panic(
            exception.NewError(
                "access control regex pattern is invalid",
                map[string]any{"pattern": normalizedPattern},
                compileErr,
            ),
        )
    }

    return Rule{
        regexPattern:  normalizedPattern,
        regexCompiled: compiledPattern,
        attributes:    normalizeAttributes(config.Attributes),
        isRegex:       true,
    }
}

func normalizeAttributes(attributes []string) []string {
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

    /* a blank attribute is refused: a rule whose attributes all normalize away would grant every authenticated principal and shadow any longer-prefixed denial */
    if 0 == len(normalizedAttributes) {
        exception.Panic(
            exception.NewError("access control rule requires at least one attribute", nil, nil),
        )
    }

    return normalizedAttributes
}

func refusePublicAccess(attributes []string, target string) {
    for _, attribute := range attributes {
        if securitycontract.AttributePublicAccess == strings.TrimSpace(attribute) {
            exception.Panic(
                exception.NewError("access control PUBLIC_ACCESS may not be declared on "+target, nil, nil),
            )
        }
    }
}

func normalizePathPrefix(pathPrefix string) string {
    normalizedPrefix := strings.TrimSpace(pathPrefix)
    if "" == normalizedPrefix {
        return ""
    }

    if "/" == normalizedPrefix {
        return "/"
    }

    return strings.TrimSuffix(normalizedPrefix, "/")
}
