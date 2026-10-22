package config

import (
    stdpath "path"
    "strings"

    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security"
)

/* BootWarning is one hazard the compiled declaration admits; the application writes each into the configured journal once, at the http boot */
type BootWarning = internal.BootWarning

const (
    bootWarningEmptyFirewallAccessControl = "security.firewallAccessControlEmpty"
    bootWarningShadowedFirewall           = "security.firewallShadowed"
    bootWarningPercentEncodedPath         = "security.pathPercentEncoded"
    bootWarningUnreachableFirewallPrefix  = "security.firewallPrefixUnreachable"
)

/* compileBootWarnings collects the hazards of a compiled declaration, in declaration order: the firewalls whose access control is the empty one the compile built, the firewalls whose path prefix no request path can carry, the firewalls an earlier one shadows, and the declared paths that carry a percent escape */
func compileBootWarnings(compiled *security.CompiledConfiguration, emptyAccessControlFirewallNames []string) []BootWarning {
    warnings := make([]BootWarning, 0)

    for _, firewallName := range emptyAccessControlFirewallNames {
        warnings = append(warnings, BootWarning{
            Name:    bootWarningEmptyFirewallAccessControl,
            Message: "the firewall declares no access control and inherits none, so the framework compiled an empty one and the firewall authorizes every path it claims for every token; declare WithAccessControl(security.NewAccessControl()) to keep it open on purpose",
            Context: loggingcontract.Context{
                "firewall": firewallName,
            },
        })
    }

    if nil == compiled {
        return warnings
    }

    warnings = append(warnings, unreachableFirewallPrefixWarnings(compiled.Firewalls())...)
    warnings = append(warnings, shadowedFirewallWarnings(compiled.Firewalls())...)
    warnings = append(warnings, percentEncodedPathWarnings(compiled)...)

    return warnings
}

/* unreachableFirewallPrefixWarnings names every path-prefix firewall whose prefix does not begin with a slash or carries an empty, "." or ".." segment: the kernel refuses a request path that is not canonical before any firewall is selected, so such a firewall is never selected, its token source and its access control never run, and its requests fall to the next firewall or to none. The two predicates are the ones the access control rule constructors refuse a path on. */
func unreachableFirewallPrefixWarnings(firewalls []*security.CompiledFirewall) []BootWarning {
    warnings := make([]BootWarning, 0)

    for _, firewall := range firewalls {
        matcher, isPrefix := firewall.Matcher().(*security.PathPrefixMatcher)
        if false == isPrefix || nil == matcher {
            continue
        }

        if false == firewallPrefixIsUnreachable(matcher.Prefix()) {
            continue
        }

        warnings = append(warnings, BootWarning{
            Name:    bootWarningUnreachableFirewallPrefix,
            Message: "the firewall is never selected: its path prefix does not begin with a slash or carries an empty, . or .. segment, and the kernel matches only a canonical request path, so its token source and access control never run; write the prefix canonical, beginning with a slash",
            Context: loggingcontract.Context{
                "firewall": firewall.Name(),
                "entry":    matcher.Prefix(),
            },
        })
    }

    return warnings
}

func firewallPrefixIsUnreachable(prefix string) bool {
    if "" == prefix || "/" == prefix {
        return false
    }

    if false == strings.HasPrefix(prefix, "/") {
        return true
    }

    /* the matcher also selects the bare spelling of a prefix written with trailing slashes, so they are dropped; a prefix without one is a string prefix, so its last segment may still grow into a canonical one ("/api/." claims "/api/.well-known") and is judged extended */
    declaredPrefix := strings.TrimRight(prefix, "/")
    if "" == declaredPrefix {
        return false
    }

    if false == strings.HasSuffix(prefix, "/") {
        declaredPrefix = declaredPrefix + "x"
    }

    return stdpath.Clean(declaredPrefix) != declaredPrefix
}

/* shadowedFirewallWarnings names every path-prefix firewall that an earlier one claims whole: the first matching firewall wins and a path prefix is a string prefix, so "/api" declared before "/api-admin" or "/api/admin" takes every request of the later one, whose access control then never runs */
func shadowedFirewallWarnings(firewalls []*security.CompiledFirewall) []BootWarning {
    warnings := make([]BootWarning, 0)

    for laterIndex, laterFirewall := range firewalls {
        laterMatcher, isLaterPrefix := laterFirewall.Matcher().(*security.PathPrefixMatcher)
        if false == isLaterPrefix || nil == laterMatcher {
            continue
        }

        for _, earlierFirewall := range firewalls[:laterIndex] {
            earlierMatcher, isEarlierPrefix := earlierFirewall.Matcher().(*security.PathPrefixMatcher)
            if false == isEarlierPrefix || nil == earlierMatcher {
                continue
            }

            if false == strings.HasPrefix(laterMatcher.Prefix(), earlierMatcher.Prefix()) {
                continue
            }

            warnings = append(warnings, BootWarning{
                Name:    bootWarningShadowedFirewall,
                Message: "the firewall is never selected: an earlier firewall's path prefix is a string prefix of its own and the first matching firewall wins, so its requests are resolved and authorized by the earlier one; declare it first, or write the earlier prefix up to a segment boundary",
                Context: loggingcontract.Context{
                    "firewall":         laterFirewall.Name(),
                    "entry":            laterMatcher.Prefix(),
                    "shadowedBy":       earlierFirewall.Name(),
                    "shadowedByPrefix": earlierMatcher.Prefix(),
                },
            })

            break
        }
    }

    return warnings
}

/* percentEncodedPathWarnings names every declared path that carries a valid percent escape other than %2F: a request path is matched decoded, so such a rule or prefix claims only the resource whose name literally holds the percent sign */
func percentEncodedPathWarnings(compiled *security.CompiledConfiguration) []BootWarning {
    warnings := make([]BootWarning, 0)

    appendWarning := func(firewallName string, declaredPath string) {
        if false == internal.CarriesPercentEscapeOtherThanSeparator(declaredPath) {
            return
        }

        warnings = append(warnings, BootWarning{
            Name:    bootWarningPercentEncodedPath,
            Message: "a declared path carries a percent escape: request paths are matched decoded, with %2F for a separator inside a segment, so the path claims only a resource whose name holds the percent sign literally; write it decoded",
            Context: loggingcontract.Context{
                "firewall": firewallName,
                "entry":    declaredPath,
            },
        })
    }

    appendRules := func(firewallName string, accessControl *security.AccessControl) {
        if nil == accessControl {
            return
        }

        /* a regex rule answers an empty path and carries its declaration as a pattern, so it is never named */
        for _, rule := range accessControl.Rules() {
            appendWarning(firewallName, rule.PathPrefix())
        }
    }

    appendRules("", compiled.GlobalAccessControl())

    for _, firewall := range compiled.Firewalls() {
        if matcher, isPrefix := firewall.Matcher().(*security.PathPrefixMatcher); true == isPrefix && nil != matcher {
            appendWarning(firewall.Name(), matcher.Prefix())
        }

        appendRules(firewall.Name(), firewall.AccessControl())
    }

    return warnings
}
