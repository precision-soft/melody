package contract

import (
    "context"
)

type Client interface {
    Get(urlString string, options RequestOptions) (Response, error)

    Post(urlString string, body any, options RequestOptions) (Response, error)

    Put(urlString string, body any, options RequestOptions) (Response, error)

    Patch(urlString string, body any, options RequestOptions) (Response, error)

    Delete(urlString string, options RequestOptions) (Response, error)

    Request(method string, urlString string, options RequestOptions) (Response, error)

    /* RequestStream hands back a response whose body is still open, bound to the context: cancelling it ends the request and the body read. The caller closes it on every path, since the stream carries no whole-stream deadline unless the request sets its Timeout to a value. */
    RequestStream(contextInstance context.Context, method string, urlString string, options RequestOptions) (StreamResponse, error)
}
