package mysql

import (
	"errors"

	driver "github.com/go-sql-driver/mysql"
)

func IsDuplicateKey(err error) bool {
	var mysqlError *driver.MySQLError
	if false == errors.As(err, &mysqlError) {
		return false
	}

	if 1062 == mysqlError.Number {
		return true
	}

	return false
}
