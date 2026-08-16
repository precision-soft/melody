package pgsql

import (
    "errors"
)

/* sqlStateCarrier is the shape of a PostgreSQL protocol error — pgdriver.Error implements it — matched as an interface because the driver exports no constructor for its error type. */
type sqlStateCarrier interface {
    Field(field byte) string
}

/* IsDuplicateKey answers on the typed SQLSTATE alone — 23505, unique_violation. A rendered message is no identity: any error whose text happens to contain those digits (a quoted value, a constraint name) would pass a substring probe, while a driver error wrapped in an exception whose message hides its cause would fail one; errors.As sees through the wrapping either way. */
func IsDuplicateKey(err error) bool {
    var carrier sqlStateCarrier
    if false == errors.As(err, &carrier) {
        return false
    }

    if "23505" == carrier.Field('C') {
        return true
    }

    return false
}
