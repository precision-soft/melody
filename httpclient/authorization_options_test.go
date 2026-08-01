package httpclient

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    httpclientcontract "github.com/precision-soft/melody/httpclient/contract"
)

/* @info this source file had no test counterpart at all, so the two setters of a basic credential were never executed by anything: an application that fills the credential in two steps — a username from configuration, a password from a secret store — went through code no test had entered, and a setter assigning the wrong field would have put the password where the user goes with nothing to say so. */
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

/* @info the setters are reachable through the option set a request is configured with — SetBasicAuth builds the credential and the setters amend it — and the halves are only distinguishable on the wire, where the two are encoded into one header. A credential amended after it was built has to reach the server as amended, or a token rotated between the declaration and the call would be sent stale. */
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
