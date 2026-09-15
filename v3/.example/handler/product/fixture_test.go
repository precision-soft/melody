package product

import (
    "bytes"
    "context"
    "github.com/precision-soft/melody/v3/.example/entity"
    "net/http/httptest"
    "encoding/json"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
    nethttp "net/http"
    "testing"
    "time"
)

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

func editorRuntime(t *testing.T) melodyruntimecontract.Runtime {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{
        values: map[string]string{melodyconfig.EnvKey: melodyconfig.EnvProduction},
    })
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    containerInstance := melodycontainer.NewContainer()

    registerConfigErr := melodycontainer.Register[melodyconfigcontract.Configuration](
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
    )
    if nil != registerConfigErr {
        t.Fatalf("register configuration: %v", registerConfigErr)
    }

    registerValidatorErr := melodycontainer.Register[*melodyvalidation.Validator](
        containerInstance,
        melodyvalidation.ServiceValidator,
        func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
            return melodyvalidation.NewValidator(), nil
        },
    )
    if nil != registerValidatorErr {
        t.Fatalf("register validator: %v", registerValidatorErr)
    }

    registerSerializerErr := melodycontainer.Register[*melodyserializer.SerializerManager](
        containerInstance,
        melodyserializer.ServiceSerializerManager,
        func(resolver melodycontainercontract.Resolver) (*melodyserializer.SerializerManager, error) {
            return melodyserializer.NewSerializerManager(
                map[string]melodyserializercontract.Serializer{
                    melodyserializer.MimeApplicationJson: melodyserializer.NewJsonSerializer(),
                },
            )
        },
    )
    if nil != registerSerializerErr {
        t.Fatalf("register serializer manager: %v", registerSerializerErr)
    }

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(
            firewall,
            melodysecurity.NewAuthenticatedToken("user-2", []string{entity.RoleUser, entity.RoleEditor}),
        ),
    )

    return runtimeInstance
}

func callCreateDoor(t *testing.T, body string) (int, string) {
    t.Helper()

    runtimeInstance := editorRuntime(t)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/products/api/create/", bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Header.Set("Accept", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("product-door-test", time.Now()),
    )

    response, handlerErr := ApiCreateHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("the door failed: %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    reader := response.BodyReader()
    if nil == reader {
        return response.StatusCode(), ""
    }

    buffer := &bytes.Buffer{}
    if _, copyErr := buffer.ReadFrom(reader); nil != copyErr {
        t.Fatalf("read body: %v", copyErr)
    }

    return response.StatusCode(), buffer.String()
}

func errorListOf(t *testing.T, body string) []string {
    t.Helper()

    var decoded struct {
        Errors []string `json:"errors"`
    }

    if decodeErr := json.Unmarshal([]byte(body), &decoded); nil != decodeErr {
        t.Fatalf("decode body %q: %v", body, decodeErr)
    }

    return decoded.Errors
}

var conversionQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

func conversionCatalogue() []*entity.Currency {
    return []*entity.Currency{
        entity.NewCurrency("cur-eur", "EUR", "Euro", 1, conversionQuoteInstant),
        entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.0842, conversionQuoteInstant),
        entity.NewCurrency("cur-ron", "RON", "Romanian Leu", 4.9761, conversionQuoteInstant),
    }
}

func conversionProduct() *entity.Product {
    return entity.NewProduct("prod-1", "probe", "probe", "cat-1", 149.99, "cur-eur", 1, conversionQuoteInstant, conversionQuoteInstant)
}

func conversionRequest(t *testing.T, target string) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, target, nil),
        map[string]string{"id": "prod-1"},
        nil,
        melodyhttp.NewRequestContext("conversion-door-test", conversionQuoteInstant),
    )
}
