package pgsql

import (
    "errors"
)

/* sqlStateCarrier is the shape of a PostgreSQL protocol error as pgdriver spells it — pgdriver.Error implements it — matched as an interface because the driver exports no constructor for its error type. */
type sqlStateCarrier interface {
    Field(field byte) string
}

/* sqlStateReporter is the same protocol error as the other PostgreSQL drivers spell it: pgx's pgconn.PgError and lib/pq's Error both answer the SQLSTATE through SQLState(), and neither carries Field. A consumer running bun over one of those drivers reached this door with a typed error this package could not see, so an insert that collided answered false and rendered as a server failure instead of a conflict. */
type sqlStateReporter interface {
    SQLState() string
}

/* IsDuplicateKey answers on the typed SQLSTATE alone — 23505, unique_violation — read through whichever of the two driver shapes the chain carries. A rendered message is no identity: any error whose text happens to contain those digits (a quoted value, a constraint name) would pass a substring probe, while a driver error wrapped in an exception whose message hides its cause would fail one; errors.As sees through the wrapping either way. */
func IsDuplicateKey(err error) bool {
    return "23505" == sqlStateOf(err)
}

/* sqlStateOf reads the SQLSTATE a PostgreSQL protocol error carries, from pgdriver's Field('C') or from the SQLState() the other drivers answer, and an empty string when the chain holds neither. */
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
