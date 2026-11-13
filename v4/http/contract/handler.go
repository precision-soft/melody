package contract

import (
    nethttp "net/http"

    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Handler func(
    runtimeInstance runtimecontract.Runtime,
    writer nethttp.ResponseWriter,
    request Request,
) (Response, error)

