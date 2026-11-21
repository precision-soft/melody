package handler

import (
    nethttp "net/http"
    "strings"

    examplejournal "github.com/precision-soft/melody/v4/.example/journal"
    "github.com/precision-soft/melody/v4/.example/presenter"
    exampleurl "github.com/precision-soft/melody/v4/.example/url"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type routeListingResponse struct {
    Name    string `json:"name"`
    Pattern string `json:"pattern"`
    Example string `json:"example"`
}

/* RoutesHandler lists the routes a client may know about, through the framework's projection every page carries, which applies the RouteAttributeExpose opt-in and the caller's zone; walking the registry directly would list internal and admin routes to an anonymous caller. */
func RoutesHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        urlGenerator := melodyhttp.UrlGeneratorMustFromRuntime(runtimeInstance)

        manifest, manifestErr := exampleurl.RouteManifestForRuntime(runtimeInstance)
        if nil != manifestErr {
            return nil, manifestErr
        }

        payload := make([]routeListingResponse, 0, len(manifest.Routes))

        for _, entry := range manifest.Routes {
            /* a route the sample values cannot fill, a requirement "1" does not meet, lists no example and is journaled, so one route never fails the listing of the others */
            examplePath, generateErr := urlGenerator.GeneratePath(entry.Name, melodyhttpcontract.PathConfig{Parameters: exampleParametersOf(entry.Pattern)})
            if nil != generateErr {
                examplePath = ""
                examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
                    "route listed without an example path",
                    melodyexception.LogContext(generateErr, melodyexception.LogContextConfig{Extra: melodyloggingcontract.Context{"route": entry.Name}}),
                )
            }

            payload = append(payload, routeListingResponse{
                Name:    entry.Name,
                Pattern: entry.Pattern,
                Example: examplePath,
            })
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

/* exampleParametersOf answers a sample value for every parameter the pattern names, so each route's example is a path it matches */
func exampleParametersOf(pattern string) map[string]string {
    parameters := map[string]string{}
    for _, segment := range strings.Split(pattern, "/") {
        if true == strings.HasPrefix(segment, ":") || true == strings.HasPrefix(segment, "*") {
            parameters[strings.TrimSuffix(segment[1:], "?")] = "1"
        }
    }

    return parameters
}

