package pgsql

import (
    "github.com/precision-soft/melody/exception"
)

type sqlStateCarrier interface {
    Field(field byte) string
}

type sqlStateReporter interface {
    SQLState() string
}

/* IsDuplicateKey answers on the typed SQLSTATE 23505 alone: a rendered message is no identity. */
func IsDuplicateKey(err error) bool {
    return "23505" == sqlStateOf(err)
}

func sqlStateOf(err error) string {
    var carrier sqlStateCarrier
    if true == exception.ChainAs(err, &carrier) {
        return carrier.Field('C')
    }

    var reporter sqlStateReporter
    if true == exception.ChainAs(err, &reporter) {
        return reporter.SQLState()
    }

    return ""
}
