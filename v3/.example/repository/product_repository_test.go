package repository

import (
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func validProduct() *entity.Product {
    return &entity.Product{
        Name:        "DDR5 32GB Dual Kit",
        Description: "black",
        CategoryId:  "cat-1",
        CurrencyId:  "cur-eur",
        Price:       0,
        Stock:       0,
    }
}

func TestValidateProductAcceptsAZeroPriceAndStock(t *testing.T) {
    if validationErr := validateProduct(validProduct()); nil != validationErr {
        t.Fatalf("expected a complete product with zero price and stock to pass, got %v", validationErr)
    }
}

func TestValidateProductNamesTheFirstFieldItFailsOn(t *testing.T) {
    testCases := map[string]struct {
        mutate   func(product *entity.Product) *entity.Product
        expected string
    }{
        "absent product":    {mutate: func(product *entity.Product) *entity.Product { return nil }, expected: "product is required"},
        "blank name":        {mutate: func(product *entity.Product) *entity.Product { product.Name = " "; return product }, expected: "name is required"},
        "blank description": {mutate: func(product *entity.Product) *entity.Product { product.Description = " "; return product }, expected: "description is required"},
        "blank category id": {mutate: func(product *entity.Product) *entity.Product { product.CategoryId = " "; return product }, expected: "category id is required"},
        "blank currency id": {mutate: func(product *entity.Product) *entity.Product { product.CurrencyId = " "; return product }, expected: "currency id is required"},
        "negative price":    {mutate: func(product *entity.Product) *entity.Product { product.Price = -1; return product }, expected: "price must be >= 0"},
        "negative stock":    {mutate: func(product *entity.Product) *entity.Product { product.Stock = -1; return product }, expected: "stock must be >= 0"},
    }

    for name, testCase := range testCases {
        t.Run(name, func(t *testing.T) {
            validationErr := validateProduct(testCase.mutate(validProduct()))
            if nil == validationErr || testCase.expected != validationErr.Error() {
                t.Fatalf("expected %q, got %v", testCase.expected, validationErr)
            }
        })
    }
}

func TestNextProductIdContinuesTheSeededNumbering(t *testing.T) {
    if "prod-8" != nextProductId([]string{"prod-1", "prod-7", "prod-3"}) {
        t.Fatalf("expected prod-8, got %q", nextProductId([]string{"prod-1", "prod-7", "prod-3"}))
    }
}
