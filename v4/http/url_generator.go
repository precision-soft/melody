package http

import (
    "net/url"
    "strings"

    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* UrlGeneratorConfig carries the public base GenerateUrl answers behind, empty for none; a base that is not an http or https url with a host name, or that carries user info, a query, a fragment or an empty, "." or ".." path segment, is refused. */
type UrlGeneratorConfig struct {
    PublicUrl string
}

func NewUrlGenerator(routeRegistry httpcontract.RouteRegistry, generatorConfig UrlGeneratorConfig) *UrlGenerator {
    publicUrl, publicUrlRefusal := internal.NormalizePublicUrl(generatorConfig.PublicUrl)
    if "" != publicUrlRefusal {
        exception.Panic(
            exception.NewError(
                "the public url is refused",
                exceptioncontract.Context{"field": "UrlGeneratorConfig.PublicUrl", "key": config.HttpPublicUrlKey, "value": generatorConfig.PublicUrl, "reason": publicUrlRefusal},
                nil,
            ),
        )
    }

    var publicBase *url.URL
    if "" != publicUrl {
        publicBase, _ = url.Parse(publicUrl)
    }

    return &UrlGenerator{
        routeRegistry: routeRegistry,
        publicBase:    publicBase,
    }
}

type UrlGenerator struct {
    routeRegistry httpcontract.RouteRegistry
    publicBase    *url.URL
}

func (instance *UrlGenerator) GeneratePath(routeName string, pathConfig httpcontract.PathOptions) (string, error) {
    generatedPath, generateErr := instance.generateRoutePath(routeName, pathConfig.Parameters)
    if nil != generateErr {
        return "", generateErr
    }

    return composeReference(url.URL{}, generatedPath, pathConfig), nil
}

func (instance *UrlGenerator) GenerateUrl(routeName string, pathConfig httpcontract.PathOptions) (string, error) {
    if nil == instance.publicBase {
        return "", exception.NewError(
            "an absolute url needs the public url",
            exceptioncontract.Context{"key": config.HttpPublicUrlKey, "routeName": routeName},
            nil,
        )
    }

    generatedPath, generateErr := instance.generateRoutePath(routeName, pathConfig.Parameters)
    if nil != generateErr {
        return "", generateErr
    }

    return composeReference(url.URL{Scheme: instance.publicBase.Scheme, Host: instance.publicBase.Host}, instance.publicBase.EscapedPath()+generatedPath, pathConfig), nil
}

/* composeReference writes the escaped path, the query without its empty key and the fragment through url.URL, so each part is escaped where it stands. */
func composeReference(reference url.URL, escapedPath string, pathConfig httpcontract.PathOptions) string {
    reference.Path, _ = url.PathUnescape(escapedPath)
    reference.RawPath = escapedPath

    queryValues := url.Values{}
    for key, values := range pathConfig.Query {
        if "" == key {
            continue
        }

        for _, value := range values {
            queryValues.Add(key, value)
        }
    }

    reference.RawQuery = queryValues.Encode()
    reference.Fragment = pathConfig.Fragment

    return reference.String()
}

func (instance *UrlGenerator) generateRoutePath(routeName string, parameters map[string]string) (string, error) {
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
                } else if "" == value && false == isCatchAll {
                    /* a single-segment wildcard spans one segment as a named parameter does, so a non-empty default fills in for a value supplied empty, as it does there */
                    defaultValue, hasDefault := defaults[wildcardName]
                    if true == hasDefault && "" != defaultValue {
                        value = defaultValue
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

                if "" == value {
                    /* an empty value would leave an empty segment, a "//" the kernel refuses before it routes, so emitting one would mint a url this router does not serve, as for a named parameter */
                    return "", exception.NewError(
                        "wildcard parameter may not be empty",
                        exceptioncontract.Context{
                            "routeName":     routeName,
                            "parameterName": wildcardName,
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

    generatedPath := "/" + strings.Join(resultParts, "/")

    /* a "." or ".." segment, or a space at the path's end, is a path the kernel refuses as non-canonical */
    if decodedPath, decodeErr := url.PathUnescape(generatedPath); nil != decodeErr || false == requestPathIsCanonical(decodedPath) {
        return "", exception.NewError(
            "generated path is not one the router serves",
            exceptioncontract.Context{
                "routeName": routeName,
                "path":      generatedPath,
            },
            nil,
        )
    }

    return generatedPath, nil
}

var _ httpcontract.UrlGenerator = (*UrlGenerator)(nil)
