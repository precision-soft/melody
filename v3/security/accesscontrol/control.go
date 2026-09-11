package accesscontrol

import (
    stdpath "path"
    "strings"
)

/* Control is a set of access control rules resolved together. Build one with NewControl. */
type Control struct {
    rules []Rule
}

/* NewControl collects the rules a request is resolved against. The caller's slice is copied, so a later write to it does not change the compiled policy. */
func NewControl(rules ...Rule) *Control {
    return &Control{
        rules: append([]Rule{}, rules...),
    }
}

/* Rules answers a copy of the rules the control was built with, in declaration order. */
func (instance *Control) Rules() []Rule {
    return append([]Rule{}, instance.rules...)
}

/* Match resolves by category before position: an exact rule beats every prefix rule, a longer prefix beats a shorter one regardless of registration order, every prefix beats every regex, and the empty-prefix fallback answers only when nothing else did. Position in the rule list — what the merge strategies order — breaks only the ties inside a category: equal-length prefixes, regexes, exact duplicates and fallbacks each resolve to the first registered.

A false second answer means no rule claimed the path. That is not a refusal: the caller decides what an unclaimed path means, and melody's own listener serves it. */
func (instance *Control) Match(path string) ([]string, bool) {
    matchedIndex, matched := instance.MatchRuleIndex(path)
    if false == matched {
        return []string{}, false
    }

    return instance.rules[matchedIndex].Attributes(), true
}

/* MatchRuleIndex answers which rule claims the path, so a caller that needs the rule itself — to report which one decided — does not resolve twice. */
func (instance *Control) MatchRuleIndex(path string) (int, bool) {
    normalizedPath := CanonicalizePath(path)

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

        if false == rule.claimsPath(normalizedPath) {
            continue
        }

        currentLength := len(rule.pathPrefix)

        if bestPrefixLength < currentLength {
            bestPrefixLength = currentLength
            bestIndex = index
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

/* claimsPath answers whether a prefix rule — raw or segment-bounded — reaches the path. */
func (instance Rule) claimsPath(normalizedPath string) bool {
    if false == strings.HasPrefix(normalizedPath, instance.pathPrefix) {
        return false
    }

    if false == instance.isSegmentPrefix {
        return true
    }

    if "/" == instance.pathPrefix {
        return true
    }

    prefixLength := len(instance.pathPrefix)

    if len(normalizedPath) == prefixLength {
        return true
    }

    return prefixLength < len(normalizedPath) && '/' == normalizedPath[prefixLength]
}

/* CanonicalizePath normalizes the path for rule lookup. HTTP callers must reject non-canonical spellings before routing or authorization: folding dot segments or surrounding whitespace can select a more permissive rule than the raw path. The HTTP kernel enforces this boundary; direct matcher callers must enforce the same agreement with their router. */
func CanonicalizePath(requestPath string) string {
    canonicalPath := strings.TrimSpace(requestPath)
    if "" == canonicalPath {
        return "/"
    }

    if false == strings.HasPrefix(canonicalPath, "/") {
        canonicalPath = "/" + canonicalPath
    }

    return stdpath.Clean(canonicalPath)
}
