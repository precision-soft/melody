package accesscontrol

import (
    "regexp"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* RuleConfig carries everything a rule declares beside its path and its matching mode. It is a struct rather than a variadic attribute list so a dimension added later — a method set, a host, an address range — arrives as a field on a call site that already compiles, instead of as another constructor. */
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

/* NewRule builds a rule with the matching mode named at the call site. It is the door a caller reaches for when the mode is chosen by a variable; when it is chosen by the code, the mode-specific constructors below say the same thing in one fewer argument.

The mode is refused when unspecified: a default would let a caller inherit a reach they never chose, and the reach is exactly what an access control rule is for. */
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

/* NewSegmentPrefixRule builds a rule bounded to a path SEGMENT: the path itself and any descendant under a "/" boundary, never a path that merely begins with the same letters. "/admin" governs "/admin" and "/admin/panel" but not "/administrator".

An empty path is refused rather than made a catch-all: an empty prefix would normalize to "" and answer for every path no other rule claimed, so a rule declared for one section would silently govern the whole application. A genuinely global rule declares "/". */
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

/* NewRawPrefixRule builds a rule that reaches across segment boundaries: every path beginning with the spelling, so "/admin" governs "/administrator" and "/admin-tools" as readily as "/admin/panel". It is the sharp tool, and PUBLIC_ACCESS is refused on it — being the longest match, a raw public rule opens every path that merely begins with the prefix, shadowing a bounded denial that would have refused.

Reach for NewSegmentPrefixRule unless the cross-segment reach is exactly what the rule means. */
func NewRawPrefixRule(path string, config RuleConfig) Rule {
    refusePublicAccess(config.Attributes, "a raw prefix rule; use a segment prefix, exact, or regex rule")

    return Rule{
        pathPrefix: normalizePathPrefix(path),
        attributes: normalizeAttributes(config.Attributes),
    }
}

/* NewRegexRule builds a rule that matches when the pattern is found ANYWHERE in the canonicalized request path. The pattern is compiled unanchored and tested with MatchString, so it is a substring match, not a whole-path one: "/public" matches "/admin/public-notes" and "/x/publications" as readily as "/public". This mirrors the path regex of other frameworks, and it is the opposite of a route requirement, which melody anchors with ^(?:…)$ — a rule meant to name one section must anchor itself. Write "^/public(/|$)" to bound it to the /public tree.

Regex rules are the lowest match priority, after exact and prefix rules, and among themselves the first registered that matches wins. */
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

    /* a rule whose attributes all normalize away still matches its path, and an empty attribute list grants every authenticated principal while shadowing any longer-prefixed rule that would have denied; the blank attribute is refused here rather than degrading the guard silently */
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
