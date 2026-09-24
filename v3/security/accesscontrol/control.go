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

/* Match resolves by category before position: an exact rule beats every prefix rule, a longer prefix beats a shorter one, every prefix beats every regex, and the empty-prefix fallback answers only when nothing else did; the position in the rule list breaks only the ties inside a category. A false second answer means no rule claimed the path, and the caller decides what that means. */
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

/* CanonicalizePath folds the spellings that reach the same resource, "//admin/panel", "/open/../admin/panel" and surrounding whitespace, into the one the rules are written in. Neither the fold nor the trim is a defence on its own: the router serves the sent spelling, so the http kernel refuses a non-canonical path before it is authorized, and a caller consulting a Control without that guard is defended by neither. */
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
