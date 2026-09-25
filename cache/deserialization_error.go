package cache

import (
    "errors"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

/* NewDeserializationError marks a cache read that found a payload the serializer cannot decode, naming the keys it was found under. Remember treats this type as a miss and recomputes, overwriting the corrupt payload, while every other error means the cache failed; a Cache of your own that wraps its deserialization failures in it inherits that. */
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
