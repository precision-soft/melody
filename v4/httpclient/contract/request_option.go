package contract

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* RequestOptions is what one request asks beyond its method and url. A field left out takes the client's behaviour: no extra header or query pair, no body, the client's timeout, no credential, and the default response body cap of 10 MiB. */
type RequestOptions struct {
    /* Headers are set over the client's own headers. Two spellings that collapse onto one header are refused, since one map has no order to pick the survivor. */
    Headers map[string]string

    /* Query pairs replace a pair of the url carrying the same name; every other pair of the url is kept byte for byte. */
    Query map[string]string

    /* Body is sent as it is, a string or a []byte; Json is marshalled and sent as application/json. Setting both is refused, and so is either on a verb that takes its body positionally. */
    Body any
    Json any

    /* Timeout bounds the whole exchange: absent is the client's timeout, unlimited no deadline. On RequestStream, absent and unlimited leave the stream without a whole-stream deadline, and a value bounds the whole stream. */
    Timeout bound.Duration

    /* BearerToken and BasicAuth write the Authorization header after every header door, so they win over an Authorization entry in Headers. Setting both is refused. */
    BearerToken string
    BasicAuth   *BasicAuth

    /* MaxResponseBodyBytes caps the response body: absent is 10 MiB, the minimum is one byte, and unlimited is refused. */
    MaxResponseBodyBytes bound.Count
}

/* BasicAuth is the username and password of a basic credential, sent as asked, empty halves included. */
type BasicAuth struct {
    Username string
    Password string
}
