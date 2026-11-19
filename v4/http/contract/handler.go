package contract

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Handler func(
    runtimeInstance containercontract.Runtime,
    writer nethttp.ResponseWriter,
    request Request,
) (Response, error)

