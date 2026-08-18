package httpclient

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    httpclientcontract "github.com/precision-soft/melody/httpclient/contract"
)

func TestBasicAuthorizationOptions_SettersAssignTheHalvesTheyName(t *testing.T) {
    basic := &BasicAuthorizationOptions{}

    basic.SetUsername("the-user")

    if "the-user" != basic.Username() {
        t.Fatalf("unexpected username: %q", basic.Username())
    }

    if "" != basic.Password() {
        t.Fatalf("expected the password to be untouched by the username setter, got %q", basic.Password())
    }

    basic.SetPassword("the-secret")

    if "the-secret" != basic.Password() {
        t.Fatalf("unexpected password: %q", basic.Password())
    }

    if "the-user" != basic.Username() {
        t.Fatalf("expected the username to survive the password setter, got %q", basic.Username())
    }
}

func TestBasicAuthorizationOptions_AmendedCredentialReachesTheRequest(t *testing.T) {
    receivedUsername := ""
    receivedPassword := ""
    credentialWasSent := false

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        receivedUsername, receivedPassword, credentialWasSent = request.BasicAuth()

        writer.WriteHeader(nethttp.StatusOK)
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    amendTheCredential := func(options httpclientcontract.RequestOptions) {
        options.SetBasicAuth("the-placeholder", "the-placeholder")

        basic := options.Authorization().Basic()
        basic.SetUsername("the-user")
        basic.SetPassword("the-secret")
    }

    response, requestErr := client.Get(server.URL, amendTheCredential)
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }

    if 200 != response.StatusCode() {
        t.Fatalf("unexpected status code: %d", response.StatusCode())
    }

    if false == credentialWasSent {
        t.Fatalf("expected the basic credential to reach the server")
    }

    if "the-user" != receivedUsername {
        t.Fatalf("expected the amended username, got %q", receivedUsername)
    }

    if "the-secret" != receivedPassword {
        t.Fatalf("expected the amended password, got %q", receivedPassword)
    }
}
