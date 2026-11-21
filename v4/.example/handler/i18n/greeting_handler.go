package i18n

import (
    "errors"
    nethttp "net/http"
    "strconv"

    "github.com/precision-soft/melody/v4/.example/presenter"
    melodybag "github.com/precision-soft/melody/v4/bag"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodytranslation "github.com/precision-soft/melody/v4/translation"
    melodytranslationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

/* bound by the openapi descriptor in config; keep it exported */
type GreetingResponse struct {
    Locale   string `json:"locale"`
    Greeting string `json:"greeting"`
    Cart     string `json:"cart"`
}

func GreetingHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        translator := melodytranslation.TranslatorMustFromRuntime(runtimeInstance)

        /* the route validated the path's locale against its list, so the one the router matched is the one served */
        locale := melodybag.StringOrDefault(request.Attributes(), melodyhttp.RouteAttributeLocale, "en")

        name := melodybag.StringOrDefault(request.Query(), "name", "")
        if "" == name {
            name = "world"
        }

        greeting, greetingErr := translate(translator, "greeting", melodytranslationcontract.TranslateConfig{Parameters: map[string]any{"name": name}, Locale: locale})
        if nil != greetingErr {
            return nil, greetingErr
        }

        cart, cartErr := translate(translator, "cart.items", melodytranslationcontract.TranslateConfig{Parameters: map[string]any{"count": queryInt(request, "count")}, Locale: locale})
        if nil != cartErr {
            return nil, cartErr
        }

        payload := GreetingResponse{
            Locale:   locale,
            Greeting: greeting,
            Cart:     cart,
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}

/* translate shows the message id for a message no catalog carries and answers every other error, so a pattern that cannot render is the request's failure rather than a half-rendered payload. */
func translate(translator melodytranslationcontract.Translator, messageId string, config melodytranslationcontract.TranslateConfig) (string, error) {
    message, translateErr := translator.Translate(messageId, config)
    if nil != translateErr && false == errors.Is(translateErr, melodytranslation.ErrMessageNotFound) {
        return "", translateErr
    }

    return message, nil
}

/* queryInt reads the count through StringOrDefault, which answers the first value of a repeated key, where bag.Int refuses a repeated key. */
func queryInt(request melodyhttpcontract.Request, name string) int {
    parsed, parseErr := strconv.Atoi(melodybag.StringOrDefault(request.Query(), name, ""))
    if nil != parseErr {
        return 0
    }

    return parsed
}
