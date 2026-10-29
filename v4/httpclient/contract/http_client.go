package contract

import (
    "context"
)

type Client interface {
    Get(contextInstance context.Context, urlString string, options RequestOptions) (Response, error)

    Post(contextInstance context.Context, urlString string, body any, options RequestOptions) (Response, error)

    Put(contextInstance context.Context, urlString string, body any, options RequestOptions) (Response, error)

    Patch(contextInstance context.Context, urlString string, body any, options RequestOptions) (Response, error)

    Delete(contextInstance context.Context, urlString string, options RequestOptions) (Response, error)

    Request(contextInstance context.Context, method string, urlString string, options RequestOptions) (Response, error)

    /* RequestStream hands back a response whose body is still open, bound to the context: cancelling it ends the request and the body read. The caller closes it on every path, since the stream carries no whole-stream deadline unless the request sets its Timeout to a value. */
    RequestStream(contextInstance context.Context, method string, urlString string, options RequestOptions) (StreamResponse, error)
}
