package product

import (
    "bytes"
    "context"
    "encoding/json"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
)

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

/* editorRuntime carries what the create door reads BEFORE it reaches the catalogue: the configuration the
   body limit comes from, the validator the binding runs, the serializer the presenter renders through, and
   an editor's token. A refused body never reaches the product service, so none is registered — a door that
   started resolving one would say so by panicking here rather than by rendering something plausible. */
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

/* the door is what has to render the per-field detail, not the presenter alone: the responder is an
   option handed to JsonHandler, so a wiring that stopped passing it would leave every probe on the
   presenter green while the client went back to reading one sentence. */
func TestApiCreateDoorAnswersOneErrorPerViolatedField(t *testing.T) {
    status, body := callCreateDoor(t, `{"id":"has space","name":"a","description":"","categoryId":"","price":-1,"currencyId":"","stock":-5}`)

    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }

    errorList := errorListOf(t, body)
    if 2 > len(errorList) {
        t.Fatalf("expected one entry per violated field, got %v", errorList)
    }

    joined := strings.Join(errorList, "\n")
    for _, expected := range []string{"id: ", "name: ", "description: ", "categoryId: ", "price: ", "currencyId: ", "stock: "} {
        if false == strings.Contains(joined, expected) {
            t.Fatalf("expected an entry for %q, got %v", expected, errorList)
        }
    }

    if true == strings.Contains(joined, "validation failed") {
        t.Fatalf("the generic message replaced the per-field detail: %v", errorList)
    }
}

/* the other half of the same responder: a body the decoder could not read carries a diagnosis that names
   internals, and this door is reachable by anyone the route lets through. */
func TestApiCreateDoorKeepsTheDecoderDiagnosisOutOfTheErrorsList(t *testing.T) {
    status, body := callCreateDoor(t, `{"name": not json`)

    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }

    errorList := errorListOf(t, body)
    if 1 != len(errorList) {
        t.Fatalf("expected exactly the public message, got %v", errorList)
    }

    if true == strings.Contains(errorList[0], "invalid character") {
        t.Fatalf("the decoder diagnosis reached the errors list: %v", errorList)
    }
}
