package url

import (
	"encoding/json"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
	melodyhttp "github.com/precision-soft/melody/v2/http"
)

type jsRouteDefinition struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

func RoutesJsonFromContainer(serviceContainer containercontract.Container) (string, error) {
	routeRegistry := melodyhttp.RouteRegistryMustFromContainer(serviceContainer)
	definitions := routeRegistry.RouteDefinitions()

	jsRoutes := make([]jsRouteDefinition, 0, len(definitions))

	for _, definition := range definitions {
		if nil == definition {
			continue
		}

		name := definition.Name()
		if "" == name {
			continue
		}

		jsRoutes = append(jsRoutes, jsRouteDefinition{
			Name:    name,
			Pattern: definition.Pattern(),
		})
	}

	payload, marshalErr := json.Marshal(jsRoutes)
	if nil != marshalErr {
		return "[]", marshalErr
	}

	return string(payload), nil
}
