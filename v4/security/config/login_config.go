package config

import (
    nethttp "net/http"
    stdpath "path"
    "strings"
    "unicode"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    httpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* LoginConfig declares the firewall's sign-in door, which the firewall serves on kernel.request before access control; the zero value declares none. Path and Handler are mandatory together, every other field has a working default. No route may claim the door's path. */
type LoginConfig struct {
    /* Path is the door's literal path, "/" first and canonical. */
    Path string

    Handler securitycontract.LoginHandler

    /* Name is the route name the url generator and the manifest resolve the door by; security.<firewall>.login when left out. */
    Name string

    /* Attributes are the door's route attributes, ExposedRouteAttributes among them, under the rules of a route's. */
    Attributes map[string]any

    /* Authenticator reads the credentials before Handler runs, and the door then rotates the session id; nil hands Handler the token source's token, and Handler rotates. */
    Authenticator securitycontract.Authenticator

    /* Methods are what the door serves, POST when left out; a safe method is refused, since a state change on one is forgeable by a link. */
    Methods []string

    /* TargetPath is where an html client is redirected when Handler answers no response, "/" when left out; a path within the application. */
    TargetPath string

    /* CrossSite is what a request a browser marks as cross-site meets, CrossSiteRefuse when left out. */
    CrossSite security.CrossSitePolicy

    /* Limiter meters the door's own requests before they authenticate; nil meters none. */
    Limiter httpcontract.RateLimiter

    /* LimiterConfig declares the key and the answer of Limiter, and is refused without it. */
    LimiterConfig httpmiddleware.RateLimitConfig
}

/* LogoutConfig declares the firewall's sign-out door, on the terms of LoginConfig; the door also ends the request's session once Handler returned. */
type LogoutConfig struct {
    Path string

    Handler securitycontract.LogoutHandler

    /* Name is security.<firewall>.logout when left out. */
    Name string

    Attributes map[string]any

    Methods []string

    TargetPath string

    CrossSite security.CrossSitePolicy
}

/* firewallDoorPathClaimer is the part of a matcher that can say at boot which paths it selects. */
type firewallDoorPathClaimer interface {
    ClaimsPath(path string) bool
}

type firewallDoorDeclaration struct {
    label             string
    declared          bool
    attributes        map[string]any
    path              string
    handlerAbsent     bool
    methods           []string
    targetPath        string
    crossSite         security.CrossSitePolicy
    authenticatorNil  bool
    limiterAbsent     bool
    limiterConfigured bool
}

func loginDoorDeclaration(config LoginConfig) firewallDoorDeclaration {
    limiterConfigured := nil != config.LimiterConfig.KeyExtractor || nil != config.LimiterConfig.OnLimitExceeded || nil != config.LimiterConfig.ClientIpResolver

    return firewallDoorDeclaration{
        label: "Login",
        /* a typed-nil handler counts as declared, so the check names it instead of the door vanishing */
        declared:          "" != config.Path || nil != config.Handler || "" != config.Name || 0 != len(config.Attributes) || nil != config.Authenticator || 0 != len(config.Methods) || "" != config.TargetPath || "" != config.CrossSite || nil != config.Limiter || true == limiterConfigured,
        path:              config.Path,
        attributes:        config.Attributes,
        handlerAbsent:     true == internal.IsNilInterface(config.Handler),
        methods:           config.Methods,
        targetPath:        config.TargetPath,
        crossSite:         config.CrossSite,
        authenticatorNil:  nil != config.Authenticator && true == internal.IsNilInterface(config.Authenticator),
        limiterAbsent:     true == internal.IsNilInterface(config.Limiter),
        limiterConfigured: limiterConfigured || (nil != config.Limiter && true == internal.IsNilInterface(config.Limiter)),
    }
}

func logoutDoorDeclaration(config LogoutConfig) firewallDoorDeclaration {
    return firewallDoorDeclaration{
        label:         "Logout",
        declared:      "" != config.Path || nil != config.Handler || "" != config.Name || 0 != len(config.Attributes) || 0 != len(config.Methods) || "" != config.TargetPath || "" != config.CrossSite,
        path:          config.Path,
        attributes:    config.Attributes,
        handlerAbsent: true == internal.IsNilInterface(config.Handler),
        methods:       config.Methods,
        targetPath:    config.TargetPath,
        crossSite:     config.CrossSite,
        limiterAbsent: true,
    }
}

func validateFirewallDoors(declaration firewallDeclaration) error {
    login := loginDoorDeclaration(declaration.config.Login)
    logout := logoutDoorDeclaration(declaration.config.Logout)

    for _, door := range []firewallDoorDeclaration{login, logout} {
        if false == door.declared {
            continue
        }

        if doorErr := validateFirewallDoor(declaration.name, door); nil != doorErr {
            return doorErr
        }
    }

    if true == login.declared && true == logout.declared && security.FirewallDoorPathForm(login.path) == security.FirewallDoorPathForm(logout.path) {
        return firewallDoorRefusal("the login and logout paths of a firewall may not coincide", declaration.name, "FirewallConfig.Logout.Path", login.path)
    }

    return nil
}

func validateFirewallDoor(firewallName string, door firewallDoorDeclaration) error {
    field := "FirewallConfig." + door.label + "."

    if "" == door.path {
        return firewallDoorRefusal("a security firewall door declares a path", firewallName, field+"Path", "")
    }

    if false == firewallDoorPathIsLiteral(door.path) {
        return firewallDoorRefusal("a security firewall door path must be literal and canonical: \"/\" first, no empty, . or .. segment, no parameter, whitespace, percent, query, fragment or backslash", firewallName, field+"Path", door.path)
    }

    if true == door.handlerAbsent {
        return firewallDoorRefusal("a security firewall door declares a handler", firewallName, field+"Handler", door.path)
    }

    if true == door.authenticatorNil {
        return firewallDoorRefusal("a security firewall door authenticator is a typed nil", firewallName, field+"Authenticator", door.path)
    }

    if true == door.limiterConfigured && true == door.limiterAbsent {
        return firewallDoorRefusal("a security firewall door limiter configuration needs a limiter", firewallName, field+"Limiter", door.path)
    }

    for key := range door.attributes {
        if true == strings.HasPrefix(key, "_") {
            return firewallDoorRefusal("a security firewall door attribute may not begin with an underscore, the prefix the framework reserves", firewallName, field+"Attributes", key)
        }
    }

    for _, method := range door.methods {
        switch strings.ToUpper(strings.TrimSpace(method)) {
        case "", nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodOptions, nethttp.MethodTrace:
            return firewallDoorRefusal("a security firewall door may not serve a safe or empty method: a state change on one is forgeable by a link", firewallName, field+"Methods", method)
        }
    }

    if "" != door.targetPath && (false == strings.HasPrefix(door.targetPath, "/") || true == http.IsExternalRedirectLocation(door.targetPath)) {
        return firewallDoorRefusal("a security firewall door target path must be a path within the application", firewallName, field+"TargetPath", door.targetPath)
    }

    if "" != door.crossSite && security.CrossSiteRefuse != door.crossSite && security.CrossSiteAdmit != door.crossSite {
        return firewallDoorRefusal("unknown security firewall door cross-site policy", firewallName, field+"CrossSite", string(door.crossSite))
    }

    return nil
}

func firewallDoorRefusal(message string, firewallName string, field string, value string) error {
    return exception.NewError(
        message,
        exceptioncontract.Context{
            "firewallName": firewallName,
            "field":        field,
            "value":        value,
        },
        nil,
    )
}

func firewallDoorPathIsLiteral(path string) bool {
    if false == strings.HasPrefix(path, "/") || true == strings.ContainsAny(path, "%?#\\") || true == strings.ContainsFunc(path, unicode.IsSpace) {
        return false
    }

    if "/" == path {
        return true
    }

    declaredPath := strings.TrimSuffix(path, "/")
    if stdpath.Clean(declaredPath) != declaredPath {
        return false
    }

    for _, segment := range strings.Split(declaredPath, "/") {
        if true == strings.HasPrefix(segment, ":") || true == strings.HasPrefix(segment, "*") {
            return false
        }
    }

    return true
}

func firewallDoorName(declaredName string, path string, firewallName string, door string) string {
    if "" == path || "" != declaredName {
        return declaredName
    }

    return "security." + firewallName + "." + door
}

func normalizeFirewallDoorMethods(methods []string) []string {
    normalized := make([]string, 0, len(methods))
    for _, method := range methods {
        normalized = append(normalized, strings.ToUpper(strings.TrimSpace(method)))
    }

    return normalized
}

func firewallDoorRateLimit(config LoginConfig) *httpmiddleware.RateLimitGate {
    if true == internal.IsNilInterface(config.Limiter) {
        return nil
    }

    return httpmiddleware.NewRateLimitGate(config.Limiter, config.LimiterConfig)
}

/* refuseUnreachableFirewallDoors refuses a door no request can reach, as far as the matchers can say at boot: its own firewall's prefix does not claim it, or an earlier firewall's does and the first matching firewall wins. */
func refuseUnreachableFirewallDoors(firewalls []*security.CompiledFirewall) error {
    for firewallIndex, firewall := range firewalls {
        for _, door := range firewall.Doors() {
            doorPath := security.FirewallDoorPathForm(door.Path)

            if claimer, canClaim := firewall.Matcher().(firewallDoorPathClaimer); true == canClaim && false == claimer.ClaimsPath(doorPath) {
                return firewallDoorRefusal("a security firewall door lies outside its firewall's prefix, so no request reaches it", firewall.Name(), door.Name, door.Path)
            }

            for _, earlierFirewall := range firewalls[:firewallIndex] {
                if claimer, canClaim := earlierFirewall.Matcher().(firewallDoorPathClaimer); true == canClaim && true == claimer.ClaimsPath(doorPath) {
                    return exception.NewError(
                        "a security firewall door lies inside an earlier firewall's prefix and the first matching firewall wins, so no request reaches it",
                        exceptioncontract.Context{
                            "firewallName": firewall.Name(),
                            "door":         door.Name,
                            "path":         door.Path,
                            "shadowedBy":   earlierFirewall.Name(),
                        },
                        nil,
                    )
                }
            }
        }
    }

    return nil
}

/* firewallDoorReachWarnings names every door whose reach a matcher cannot state at boot. */
func firewallDoorReachWarnings(firewalls []*security.CompiledFirewall) []BootWarning {
    warnings := make([]BootWarning, 0)

    for firewallIndex, firewall := range firewalls {
        for _, door := range firewall.Doors() {
            unknownMatcher := ""

            for _, candidate := range firewalls[:firewallIndex+1] {
                if _, canClaim := candidate.Matcher().(firewallDoorPathClaimer); false == canClaim {
                    unknownMatcher = candidate.Name()

                    break
                }
            }

            if "" == unknownMatcher {
                continue
            }

            warnings = append(warnings, BootWarning{
                Name:    internal.BootWarningNameFirewallDoorReachUnknown,
                Message: "a firewall door's reach is not known at boot: a matcher that is no prefix may select another firewall for its path, and an unserved request there is a 404",
                Context: loggingcontract.Context{
                    "firewall": firewall.Name(),
                    "entry":    door.Path,
                    "matcher":  unknownMatcher,
                },
            })
        }
    }

    return warnings
}
