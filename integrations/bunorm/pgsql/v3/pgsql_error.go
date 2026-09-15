package pgsql

import (
    "errors"
)

type sqlStateCarrier interface {
    Field(field byte) string
}

type sqlStateReporter interface {
    SQLState() string
}

/* IsDuplicateKey answers on the typed SQLSTATE alone — 23505, unique_violation — read through whichever of the two driver shapes the chain carries. A rendered message is no identity: any error whose text happens to contain those digits (a quoted value, a constraint name) would pass a substring probe, while a driver error wrapped in an exception whose message hides its cause would fail one; errors.As sees through the wrapping either way. */
func IsDuplicateKey(err error) bool {
    return "23505" == sqlStateOf(err)
}

func sqlStateOf(err error) string {
    var carrier sqlStateCarrier
    if true == errors.As(err, &carrier) {
        return carrier.Field('C')
    }

    var reporter sqlStateReporter
    if true == errors.As(err, &reporter) {
        return reporter.SQLState()
    }

    return ""
}
