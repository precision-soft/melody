package contract

import (
    "time"
)

type RequestOption func(RequestOptions)

type RequestOptions interface {
    Headers() map[string]string

    Query() map[string]string

    Body() any

    ContentType() string

    Timeout() time.Duration

    Authorization() AuthorizationOptions

    MaxResponseBodyBytes() int

    SetMaxResponseBodyBytes(maxResponseBodyBytes int)

    SetHeader(key string, value string)

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
