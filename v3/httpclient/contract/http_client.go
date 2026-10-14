package contract

type Client interface {
    Get(urlString string, options ...RequestOption) (Response, error)

    Post(urlString string, body any, options ...RequestOption) (Response, error)

    Put(urlString string, body any, options ...RequestOption) (Response, error)

    Patch(urlString string, body any, options ...RequestOption) (Response, error)

    Delete(urlString string, options ...RequestOption) (Response, error)

    Request(method string, urlString string, options ...RequestOption) (Response, error)

    /* RequestStream hands back a response whose body is still open. The caller closes it on every path, since the streaming path carries no whole-request deadline unless the request sets a non-zero timeout: a positive one bounds the whole stream, and a negative one binds it to the client's own timeout. */
    RequestStream(method string, urlString string, options ...RequestOption) (StreamResponse, error)
}
