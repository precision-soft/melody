package i18n

import (
    nethttp "net/http"
    "strconv"

    "github.com/precision-soft/melody/v3/.example/presenter"
    melodybag "github.com/precision-soft/melody/v3/bag"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodytranslation "github.com/precision-soft/melody/v3/translation"
)

/* bound by the openapi descriptor in config; keep it exported */
type GreetingResponse struct {
    Locale   string `json:"locale"`
    Greeting string `json:"greeting"`
    Cart     string `json:"cart"`
}

func GreetingHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        translator := melodytranslation.TranslatorMustFromContainer(runtimeInstance.Container())

        locale := melodybag.StringOrDefault(request.Query(), "locale", "")
        if "" == locale {
            locale = "en"
        }

        name := melodybag.StringOrDefault(request.Query(), "name", "")
        if "" == name {
            name = "world"
        }

        payload := GreetingResponse{
            Locale:   locale,
            Greeting: translator.Trans("greeting", map[string]any{"name": name}, "messages", locale),
            Cart:     translator.Trans("cart.items", map[string]any{"count": queryInt(request, "count")}, "messages", locale),
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

/* queryInt reads the count through StringOrDefault, which answers the first value of a repeated key, where bag.Int refuses a repeated key. */
func queryInt(request melodyhttpcontract.Request, name string) int {
    parsed, parseErr := strconv.Atoi(melodybag.StringOrDefault(request.Query(), name, ""))
    if nil != parseErr {
        return 0
    }

    return parsed
}
