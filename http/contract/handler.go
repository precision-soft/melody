package contract

import (
    nethttp "net/http"

    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type Handler func(
    runtimeInstance runtimecontract.Runtime,
    writer nethttp.ResponseWriter,
    request Request,
) (Response, error)

type ErrorHandler func(
    runtime runtimecontract.Runtime,
    writer nethttp.ResponseWriter,
    request Request,
    err error,
) Response
