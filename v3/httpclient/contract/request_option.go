package contract

import (
    "time"
)

type RequestOption func(RequestOptions)

type RequestOptions interface {
    /* Headers answers a copy of the request headers: the setters are the one door that writes, so a write into the returned map reaches nothing. */
    Headers() map[string]string

    /* Query answers a copy of the query parameters, under the same single-door rule as Headers. */
    Query() map[string]string

    Body() any

    ContentType() string

    Timeout() time.Duration

    Authorization() AuthorizationOptions

    MaxResponseBodyBytes() int

    SetMaxResponseBodyBytes(maxResponseBodyBytes int)

    SetHeader(key string, value string)

    /* SetHeaders refuses a map carrying two spellings of one header, and the refusal is answered by the melody client alone: the door has no error to return, so the option set keeps the refusal and the client's request path fails the request naming the option. Another consumer of a RequestOptions value — a client of its own, a decorator, a double — sees an unwritten map and no refusal, since nothing on this contract carries it. */
    SetHeaders(headers map[string]string)

    SetQuery(key string, value string)

    SetQueryParams(parameters map[string]string)

    SetBody(body any)

    SetJson(data any)

    SetTimeout(timeout time.Duration)

    SetBearerToken(token string)

    SetBasicAuth(username string, password string)
}

type AuthorizationOptions interface {
    Bearer() string

    SetBearer(bearer string)

    Basic() BasicAuthorizationOptions

    SetBasic(basic BasicAuthorizationOptions)
}

type BasicAuthorizationOptions interface {
    Username() string

    SetUsername(username string)

    Password() string

    SetPassword(password string)
}
