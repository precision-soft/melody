package mysql

import (
    driver "github.com/go-sql-driver/mysql"

    "github.com/precision-soft/melody/v4/exception"
)

func IsDuplicateKey(err error) bool {
    var mysqlError *driver.MySQLError
    if false == exception.ChainAs(err, &mysqlError) {
        return false
    }

    if 1062 == mysqlError.Number {
        return true
    }

    return false
}
