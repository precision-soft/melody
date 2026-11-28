package http

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* RouteOptions is the contract's type under this package's name, the one Handle takes. */
type RouteOptions = httpcontract.RouteOptions

/* RouteProtectionConfig is the contract's type under this package's name. */
type RouteProtectionConfig = httpcontract.RouteProtectionConfig

/* HmacCheckOrder is the contract's type under this package's name. */
type HmacCheckOrder = httpcontract.HmacCheckOrder

const (
    HmacCheckOrderInherited  = httpcontract.HmacCheckOrderInherited
    HmacCheckOrderNonceFirst = httpcontract.HmacCheckOrderNonceFirst
    HmacCheckOrderBodyFirst  = httpcontract.HmacCheckOrderBodyFirst
)
