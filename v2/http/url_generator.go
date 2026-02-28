package http

import (
    "net/url"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
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

            resultParts = append(resultParts, value)

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

                resultParts = append(resultParts, value)

                continue
            }

            if "" == wildcardName {
                if false == hasValue {
                    value = ""
                    hasValue = true
                }
            }

            if true == hasValue {
                value = strings.Trim(value, "/")
                if "" == value {
                    continue
                }

                segments := strings.Split(value, "/")
                for _, segment := range segments {
                    if "" == segment {
                        continue
                    }

                    resultParts = append(resultParts, segment)
                }
            }

            continue
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
