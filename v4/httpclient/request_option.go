package httpclient

import (
    "maps"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpclientcontract "github.com/precision-soft/melody/v4/httpclient/contract"
)

/* RequestOptions is the contract's options struct, named here so a caller of the client needs no second import. */
type RequestOptions = httpclientcontract.RequestOptions

/* BasicAuth is the contract's basic credential, named here for the same reason. */
type BasicAuth = httpclientcontract.BasicAuth

const defaultMaxResponseBodyBytes = 10 * 1024 * 1024

var (
    requestTimeoutRule = bound.Rule[time.Duration]{
        Name:              "RequestOptions.Timeout",
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }

    maxResponseBodyBytesRule = bound.Rule[int]{
        Name:    "RequestOptions.MaxResponseBodyBytes",
        Default: defaultMaxResponseBodyBytes,
        Minimum: 1,
    }
)

/* resolvedRequestOptions is one request's options with every field decided and every map copied, so a caller writing its maps after the call reaches nothing. */
type resolvedRequestOptions struct {
    headers              map[string]string
    query                map[string]string
    body                 any
    json                 bool
    timeout              bound.Duration
    bearerToken          string
    basicAuth            *BasicAuth
    maxResponseBodyBytes int
}

/* resolveRequestOptions decides the options before anything is dialled, so a refused option never follows a committed side effect a caller would retry. A verb that takes its body positionally hands it as positionalJson, which refuses a Body or a Json beside it. */
func resolveRequestOptions(options RequestOptions, positionalJson any, hasPositionalJson bool) (resolvedRequestOptions, error) {
    headers, headersErr := canonicalizeHeaderMap(options.Headers)
    if nil != headersErr {
        return resolvedRequestOptions{}, headersErr
    }

    if nil != options.Body && nil != options.Json {
        return resolvedRequestOptions{}, exception.NewError(
            "the request body is given twice",
            exceptioncontract.Context{
                "field": "RequestOptions.Json",
            },
            nil,
        )
    }

    if true == hasPositionalJson && (nil != options.Body || nil != options.Json) {
        field := "RequestOptions.Body"
        if nil != options.Json {
            field = "RequestOptions.Json"
        }

        return resolvedRequestOptions{}, exception.NewError(
            "the request body is given twice",
            exceptioncontract.Context{
                "field": field,
            },
            nil,
        )
    }

    if "" != options.BearerToken && nil != options.BasicAuth {
        return resolvedRequestOptions{}, exception.NewError(
            "the request names two credentials",
            exceptioncontract.Context{
                "field": "RequestOptions.BasicAuth",
            },
            nil,
        )
    }

    if _, timeoutErr := bound.Resolve(options.Timeout, requestTimeoutRule); nil != timeoutErr {
        return resolvedRequestOptions{}, timeoutErr
    }

    maxResponseBodyBytes, capErr := bound.Resolve(options.MaxResponseBodyBytes, maxResponseBodyBytesRule)
    if nil != capErr {
        return resolvedRequestOptions{}, capErr
    }

    resolved := resolvedRequestOptions{
        headers:              headers,
        query:                maps.Clone(options.Query),
        body:                 options.Body,
        timeout:              options.Timeout,
        bearerToken:          options.BearerToken,
        maxResponseBodyBytes: maxResponseBodyBytes.Value,
    }

    if nil != options.Json {
        resolved.body = options.Json
        resolved.json = true
    }

    if true == hasPositionalJson {
        resolved.body = positionalJson
        resolved.json = true
    }

    if nil != options.BasicAuth {
        basicAuth := *options.BasicAuth
        resolved.basicAuth = &basicAuth
    }

    return resolved, nil
}
