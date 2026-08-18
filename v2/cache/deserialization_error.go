package cache

import (
    "errors"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

/* NewDeserializationError marks a cache read that found a payload the serializer cannot decode, naming the keys it happened under. The type matters more than the message: Remember treats an error of this type as a miss and recomputes, overwriting the corrupt payload, while every other error keeps meaning the cache itself failed. A Cache implementation of your own that wraps its deserialization failures in it inherits that self-healing. */
func NewDeserializationError(keys []string, causeErr error) *DeserializationError {
    return &DeserializationError{
        exceptionErr: exception.NewError(
            "cache payload deserialization failed",
            exceptioncontract.Context{
                "keys": keys,
            },
            causeErr,
        ),
    }
}

type DeserializationError struct {
    exceptionErr *exception.Error
}

func (instance *DeserializationError) Error() string {
    return instance.exceptionErr.Error()
}

func (instance *DeserializationError) Unwrap() error {
    return instance.exceptionErr
}

func IsDeserializationError(err error) bool {
    var deserializationErr *DeserializationError

    return errors.As(err, &deserializationErr)
}
