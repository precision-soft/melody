package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    exampleurl "github.com/precision-soft/melody/v3/.example/url"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type routeListingResponse struct {
    Name    string `json:"name"`
    Pattern string `json:"pattern"`
    Example string `json:"example"`
}

/* RoutesHandler lists only exposed routes in the caller’s zone, using the same projection as page rendering. */
func RoutesHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        urlGenerator := melodyhttp.UrlGeneratorMustFromContainer(runtimeInstance.Container())

        manifest := exampleurl.RouteManifestForRuntime(runtimeInstance)
        payload := make([]routeListingResponse, 0, len(manifest.Routes))

        for _, entry := range manifest.Routes {
            examplePath, _ := urlGenerator.GeneratePath(
                entry.Name,
                map[string]string{
                    "id": "1",
                },
            )

            payload = append(payload, routeListingResponse{
                Name:    entry.Name,
                Pattern: entry.Pattern,
                Example: examplePath,
            })
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
