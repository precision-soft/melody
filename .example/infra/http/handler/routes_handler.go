package handler

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/.example/infra/http/presenter"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type routeDemoResponse struct {
    Name    string `json:"name"`
    Pattern string `json:"pattern"`
    Example string `json:"example"`
}

func RoutesHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        routeRegistry := melodyhttp.RouteRegistryMustFromContainer(runtimeInstance.Container())
        urlGenerator := melodyhttp.UrlGeneratorMustFromContainer(runtimeInstance.Container())

        definitions := routeRegistry.RouteDefinitions()
        payload := make([]routeDemoResponse, 0, len(definitions))

        for _, definition := range definitions {
            if nil == definition {
                continue
            }

            name := strings.TrimSpace(definition.Name())
            if "" == name {
                continue
            }

            examplePath, _ := urlGenerator.GeneratePath(
                name,
                map[string]string{
                    "id": "1",
                },
            )

            payload = append(payload, routeDemoResponse{
                Name:    name,
                Pattern: definition.Pattern(),
                Example: examplePath,
            })
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
