package repository

import (
    "errors"
    "strings"

    "github.com/precision-soft/melody/v3/.example/migration"
)

/* ErrCurrencyInUse is the refusal a currency delete answers while a product is priced in it, whether the service's read or the product table's foreign key caught it, so the door answers 409 rather than leaving the products quoted in a currency the catalogue lost. */
var ErrCurrencyInUse = errors.New("a product is priced in the currency")

/* ErrUnknownCurrency and ErrUnknownCategory are the refusals a product write answers for a reference that names nothing, whether the service's read or the foreign key caught it, so the door answers 400. */
var (
    ErrUnknownCurrency = errors.New("the currency does not exist")
    ErrUnknownCategory = errors.New("the category does not exist")
)

/* asProductReferenceRefusal maps the product table's foreign key refusals of a write onto the reference that named nothing; any other failure is answered untouched */
func asProductReferenceRefusal(writeErr error) error {
    if nil == writeErr {
        return nil
    }

    if true == errorChainNamesForeignKey(writeErr, migration.ProductCurrencyForeignKeyName) {
        return ErrUnknownCurrency
    }

    if true == errorChainNamesForeignKey(writeErr, migration.ProductCategoryForeignKeyName) {
        return ErrUnknownCategory
    }

    return writeErr
}

/* asCurrencyInUse maps the refusal of a currency delete by the product table's foreign key onto ErrCurrencyInUse */
func asCurrencyInUse(deleteErr error) error {
    if nil == deleteErr {
        return nil
    }

    if false == errorChainNamesForeignKey(deleteErr, migration.ProductCurrencyForeignKeyName) {
        return deleteErr
    }

    return ErrCurrencyInUse
}

/* errorChainNamesForeignKey answers whether any link of the chain is MySQL's foreign key refusal naming the constraint given, the 1451 of a parent's delete or the 1452 of a child's write, both of which spell the constraint as CONSTRAINT `<name>` */
func errorChainNamesForeignKey(err error, constraintName string) bool {
    return errorChainHolds(err, func(text string) bool {
        return true == strings.Contains(text, "foreign key constraint fails") && true == strings.Contains(text, "CONSTRAINT `"+constraintName+"`")
    })
}
