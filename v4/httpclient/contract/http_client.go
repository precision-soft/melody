package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* Client is the contract an http client implements: every door takes the runtime it runs under first, refused when nil or carrying no context, the request built over the runtime's context. Userland calls it through httpclient.Bound, bound once to its request, run or delivery. */
type Client interface {
    Get(runtimeInstance containercontract.Runtime, urlString string, options RequestOptions) (Response, error)

    Post(runtimeInstance containercontract.Runtime, urlString string, body any, options RequestOptions) (Response, error)

    Put(runtimeInstance containercontract.Runtime, urlString string, body any, options RequestOptions) (Response, error)

    Patch(runtimeInstance containercontract.Runtime, urlString string, body any, options RequestOptions) (Response, error)

    Delete(runtimeInstance containercontract.Runtime, urlString string, options RequestOptions) (Response, error)

    Request(runtimeInstance containercontract.Runtime, method string, urlString string, options RequestOptions) (Response, error)

    /* RequestStream hands back a response whose body is still open, bound to the runtime's context: cancelling it ends the request and the body read. The caller closes it on every path, since the stream carries no whole-stream deadline unless the request sets its Timeout to a value. */
    RequestStream(runtimeInstance containercontract.Runtime, method string, urlString string, options RequestOptions) (StreamResponse, error)
}
