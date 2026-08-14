package repository

import (
    "time"

    "github.com/precision-soft/melody/.example/entity"
)

/* The shared test material of the package lives here, and only here: this is the one test file the layout rule exempts from having a source of its own. */

/* fixtureTime is a fixed stamp, so an entity built for a validation assertion never carries a value that changes between runs. */
var fixtureTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

/* validProduct is a product every field of validateProduct accepts. Each assertion blanks exactly ONE field of it, so a refusal names the field the assertion is about rather than whichever one happens to be checked first. */
func validProduct() *entity.Product {
    return entity.NewProduct(
        "prod-1",
        "Keyboard",
        "A keyboard",
        "cat-1",
        49.99,
        "cur-1",
        3,
        fixtureTime,
        fixtureTime,
    )
}

func validUser() *entity.User {
    return entity.NewUser("user-1", "user", "hash", []string{entity.RoleUser})
}

func validCategory() *entity.Category {
    return entity.NewCategory("cat-1", "Peripherals")
}

func validCurrency() *entity.Currency {
    return entity.NewCurrency("cur-1", "EUR", "Euro")
}
