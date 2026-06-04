package i18n

import (
    nethttp "net/http"
    "strconv"

    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodytranslation "github.com/precision-soft/melody/v3/translation"
)

type greetingPayload struct {
    Locale   string `json:"locale"`
    Greeting string `json:"greeting"`
    Cart     string `json:"cart"`
}

func GreetingHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        translator := melodytranslation.TranslatorMustFromContainer(runtimeInstance.Container())

        locale := queryString(request, "locale")
        if "" == locale {
            locale = "en"
        }

        name := queryString(request, "name")
        if "" == name {
            name = "world"
        }

        payload := greetingPayload{
            Locale:   locale,
            Greeting: translator.Trans("greeting", map[string]any{"name": name}, "messages", locale),
            Cart:     translator.Trans("cart.items", map[string]any{"count": queryInt(request, "count")}, "messages", locale),
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

func queryString(request melodyhttpcontract.Request, name string) string {
    value, exists := request.Query().Get(name)
    if false == exists {
        return ""
    }

    switch typed := value.(type) {
    case string:
        return typed
    case []string:
        if 0 == len(typed) {
            return ""
        }
        return typed[0]
    default:
        return ""
    }
}

func queryInt(request melodyhttpcontract.Request, name string) int {
    parsed, parseErr := strconv.Atoi(queryString(request, name))
    if nil != parseErr {
        return 0
    }

    return parsed
}
