package repository

import "testing"

/* validateProduct reports the FIRST field the product fails on, and both implementations share it so the in-memory catalogue and the database refuse the same writes with the same words. Each case blanks one field of an otherwise valid product, so the message pins which guard answered rather than which one happens to come first. */
func TestValidateProductNamesTheFieldThatFailed(t *testing.T) {
    for expected, breakIt := range map[string]func(*testing.T) error{
        "name is required": func(t *testing.T) error {
            product := validProduct()
            product.Name = "   "

            return validateProduct(product)
        },
        "description is required": func(t *testing.T) error {
            product := validProduct()
            product.Description = ""

            return validateProduct(product)
        },
        "category id is required": func(t *testing.T) error {
            product := validProduct()
            product.CategoryId = ""

            return validateProduct(product)
        },
        "currency id is required": func(t *testing.T) error {
            product := validProduct()
            product.CurrencyId = ""

            return validateProduct(product)
        },
        "price must be >= 0": func(t *testing.T) error {
            product := validProduct()
            product.Price = -0.01

            return validateProduct(product)
        },
        "stock must be >= 0": func(t *testing.T) error {
            product := validProduct()
            product.Stock = -1

            return validateProduct(product)
        },
        "product is required": func(t *testing.T) error {
            return validateProduct(nil)
        },
    } {
        t.Run(expected, func(t *testing.T) {
            err := breakIt(t)

            if nil == err {
                t.Fatalf("the product was accepted")
            }

            if expected != err.Error() {
                t.Fatalf("got %q, want %q", err.Error(), expected)
            }
        })
    }
}

func TestValidateProductAcceptsTheBoundaryValues(t *testing.T) {
    product := validProduct()
    product.Price = 0
    product.Stock = 0

    if err := validateProduct(product); nil != err {
        t.Fatalf("a free product with no stock was refused: %v", err)
    }
}

func TestNextProductIdContinuesTheSeededNumbering(t *testing.T) {
    if "prod-4" != nextProductId([]string{"prod-1", "prod-3", "prod-2"}) {
        t.Fatalf("unexpected identifier: %q", nextProductId([]string{"prod-1", "prod-3", "prod-2"}))
    }

    if "prod-1" != nextProductId(nil) {
        t.Fatalf("an empty catalogue must start at one, got %q", nextProductId(nil))
    }

    /* another kind's identifiers carry a different prefix, so they cannot move this counter */
    if "prod-1" != nextProductId([]string{"cat-9", "cur-7"}) {
        t.Fatalf("a foreign prefix moved the counter: %q", nextProductId([]string{"cat-9", "cur-7"}))
    }
}
