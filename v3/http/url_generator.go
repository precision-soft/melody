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
                /* a non-empty default also fills in for a parameter supplied empty, which is what lets rejectNonTrailingOptionalParameter admit a non-trailing optional */
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

                /* matchPath refuses an empty segment for a named parameter */
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
                /* a ":param" spans one path segment, so a slash is refused rather than escaped as "%2F", a spelling a proxy in front may decode into a separator; the single-segment wildcard branch below refuses it the same way */
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
                /* the requirement is tested on the remainder matchPath receives, the non-empty segments joined by "/", so "a//b" is checked as "a/b" */
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

            /* a catch-all is terminal in registration and matching, so nothing after it is emitted */
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
