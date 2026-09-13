package contract

type Client interface {
    Get(urlString string, options ...RequestOption) (Response, error)

    Post(urlString string, body any, options ...RequestOption) (Response, error)

    Put(urlString string, body any, options ...RequestOption) (Response, error)

    Patch(urlString string, body any, options ...RequestOption) (Response, error)

    Delete(urlString string, options ...RequestOption) (Response, error)

    Request(method string, urlString string, options ...RequestOption) (Response, error)

    /* RequestStream hands back a response whose body is still open. The caller closes it on every path — the streaming path carries no whole-request deadline, so a stream left unclosed holds its connection for the life of the process. */
    RequestStream(method string, urlString string, options ...RequestOption) (StreamResponse, error)
}
