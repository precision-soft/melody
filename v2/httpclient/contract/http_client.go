package contract

type Client interface {
	Get(urlString string, options ...RequestOption) (Response, error)

	Post(urlString string, body any, options ...RequestOption) (Response, error)

	Put(urlString string, body any, options ...RequestOption) (Response, error)

	Patch(urlString string, body any, options ...RequestOption) (Response, error)

	Delete(urlString string, options ...RequestOption) (Response, error)

	Request(method string, urlString string, options ...RequestOption) (Response, error)

	RequestStream(method string, urlString string, options ...RequestOption) (StreamResponse, error)
}
