package http

import (
    "net/url"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func NewUrlGenerator(routeRegistry httpcontract.RouteRegistry) *UrlGenerator {
    return &UrlGenerator{
        routeRegistry: routeRegistry,
    }
}

type UrlGenerator struct {
    routeRegistry httpcontract.RouteRegistry
}

func (instance *UrlGenerator) GeneratePath(routeName string, parameters map[string]string) (string, error) {
    routeDefinition, exists := instance.routeRegistry.RouteDefinitionForUrlGeneration(routeName)
    if false == exists {
        return "", exception.NewError(
            "route not found",
            exceptioncontract.Context{
                "routeName": routeName,
            },
            nil,
        )
    }

    patternParts := splitPath(routeDefinition.Pattern())
    resultParts := make([]string, 0, len(patternParts))

    defaults := routeDefinition.Defaults()
    requirements := routeDefinition.Requirements()

    for index, part := range patternParts {
        if 0 == index {
            continue
        }

        if true == strings.HasPrefix(part, ":") {
            paramName := strings.TrimPrefix(part, ":")
            isOptional := false
            if true == strings.HasSuffix(paramName, "?") {
                isOptional = true
                paramName = strings.TrimSuffix(paramName, "?")
            }

            value, exists := parameters[paramName]
            if false == exists {
                defaultValue, hasDefault := defaults[paramName]
                if true == hasDefault {
                    value = defaultValue
                    exists = true
                }
            } else if "" == value {
                /* a non-empty default also fills in for a parameter supplied EMPTY, not only for an absent one: rejectNonTrailingOptionalParameter admits a non-trailing optional exactly because its default keeps the segment present, and the natural caller passes the current locale, which is sometimes "". Dropping the segment here would mint a url matchPath binds one segment out of step and answers with a 404. */
                defaultValue, hasDefault := defaults[paramName]
                if true == hasDefault && "" != defaultValue {
                    value = defaultValue
                }
            }

            if false == exists {
                if true == isOptional {
                    continue
                }

                return "", exception.NewError(
                    "route parameter missing",
                    exceptioncontract.Context{
                        "routeName":           routeName,
                        "parameterName":       paramName,
                        "availableParameters": parameters,
                    },
                    nil,
                )
            }

            if "" == value {
                if true == isOptional {
                    continue
                }

                /* matchPath refuses an empty segment for a named parameter, so emitting one here would mint a url this router answers with a 404 */
                return "", exception.NewError(
                    "route parameter may not be empty",
                    exceptioncontract.Context{
                        "routeName":     routeName,
                        "parameterName": paramName,
                    },
                    nil,
                )
            }

            if true == strings.Contains(value, "/") {
                /* a ":param" spans exactly one path segment, and matchPath binds it to a single segment (never a "/"-joined remainder); emitting url.PathEscape here would encode the slash as %2F, which the net/http server decodes back to "/" before the kernel matches on request.URL.Path, so the generated link would resolve to a different route or 404. Reject the slash instead of minting a url this router cannot answer, mirroring the single-segment wildcard branch below. */
                return "", exception.NewError(
                    "route parameter value cannot contain slash",
                    exceptioncontract.Context{
                        "routeName":     routeName,
                        "parameterName": paramName,
                        "value":         value,
                    },
                    nil,
                )
            }

            if regex, exists := requirements[paramName]; true == exists {
                if false == regex.MatchString(value) {
                    return "", exception.NewError(
                        "route parameter requirement failed",
                        exceptioncontract.Context{
                            "routeName":     routeName,
                            "parameterName": paramName,
                            "value":         value,
                        },
                        nil,
                    )
                }
            }

            resultParts = append(resultParts, url.PathEscape(value))

            continue
        }

        if true == strings.HasPrefix(part, "*") {
            wildcardName := strings.TrimPrefix(part, "*")
            isCatchAll := false
            if true == strings.HasSuffix(wildcardName, "...") {
                isCatchAll = true
                wildcardName = strings.TrimSuffix(wildcardName, "...")
            }

            if len(patternParts)-1 == index {
                isCatchAll = true
            }

            value := ""
            hasValue := false
            if "" != wildcardName {
                value, hasValue = parameters[wildcardName]
                if false == hasValue {
                    defaultValue, hasDefault := defaults[wildcardName]
                    if true == hasDefault {
                        value = defaultValue
                        hasValue = true
                    }
                }
            }

            if false == isCatchAll {
                if "" == wildcardName {
                    return "", exception.NewError(
                        "wildcard segment must be named for url generation",
                        exceptioncontract.Context{
                            "routeName": routeName,
                            "pattern":   routeDefinition.Pattern(),
                        },
                        nil,
                    )
                }

                if false == hasValue {
                    return "", exception.NewError(
                        "wildcard parameter missing",
                        exceptioncontract.Context{
                            "routeName":           routeName,
                            "parameterName":       wildcardName,
                            "availableParameters": parameters,
                        },
                        nil,
                    )
                }

                if true == strings.Contains(value, "/") {
                    return "", exception.NewError(
                        "wildcard segment value cannot contain slash",
                        exceptioncontract.Context{
                            "routeName":     routeName,
                            "parameterName": wildcardName,
                            "value":         value,
                        },
                        nil,
                    )
                }

                if regex, exists := requirements[wildcardName]; true == exists {
                    if false == regex.MatchString(value) {
                        return "", exception.NewError(
                            "wildcard parameter requirement failed",
                            exceptioncontract.Context{
                                "routeName":     routeName,
                                "parameterName": wildcardName,
                                "value":         value,
                            },
                            nil,
                        )
                    }
                }

                resultParts = append(resultParts, url.PathEscape(value))

                continue
            }

            if "" == wildcardName {
                if false == hasValue {
                    value = ""
                    hasValue = true
                }
            }

            catchAllSegments := make([]string, 0)
            if true == hasValue {
                for _, segment := range strings.Split(value, "/") {
                    if "" == segment {
                        continue
                    }

                    catchAllSegments = append(catchAllSegments, segment)
                }
            }

            if regex, exists := requirements[wildcardName]; true == exists {
                /* matchPath tests the same regex against the remainder it actually receives — the emitted, non-empty segments joined by "/" — so the check must run on that collapsed remainder, not the merely edge-trimmed value: "a//b" emits "a/b", which matchPath accepts, so the generator must not refuse it. A requirement on a catch-all is a whitelist, the one place a traversal like "../../etc/passwd" is meant to be caught. */
                if false == regex.MatchString(strings.Join(catchAllSegments, "/")) {
                    return "", exception.NewError(
                        "catch-all parameter requirement failed",
                        exceptioncontract.Context{
                            "routeName":     routeName,
                            "parameterName": wildcardName,
                            "value":         value,
                        },
                        nil,
                    )
                }
            }

            for _, segment := range catchAllSegments {
                resultParts = append(resultParts, url.PathEscape(segment))
            }

            /* a catch-all consumes the rest of the path: registerRouteInTree and matchPath both treat "*name..." as terminal and ignore any pattern segments that follow it, so the generator must stop here rather than append those trailing literals — emitting them would mint a url this router answers with a 404 */
            break
        }

        resultParts = append(resultParts, part)
    }

    if 0 == len(resultParts) {
        return "/", nil
    }

    return "/" + strings.Join(resultParts, "/"), nil
}

func (instance *UrlGenerator) GenerateUrl(routeName string, params map[string]string, queryParams map[string]string) (string, error) {
    pathValue, err := instance.GeneratePath(routeName, params)
    if nil != err {
        return "", err
    }

    queryValues := url.Values{}
    for key, value := range queryParams {
        if "" == key {
            continue
        }

        queryValues.Set(key, value)
    }

    queryString := queryValues.Encode()
    if "" == queryString {
        return pathValue, nil
    }

    return pathValue + "?" + queryString, nil
}

var _ httpcontract.UrlGenerator = (*UrlGenerator)(nil)
