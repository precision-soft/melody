package cli

import (
    "fmt"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const productListFlagLimit = "limit"

const productListDefaultLimit = 5

type ProductListCommand struct{}

func NewProductListCommand() *ProductListCommand {
    return &ProductListCommand{}
}

func (instance *ProductListCommand) Name() string {
    return "product:list"
}

func (instance *ProductListCommand) Description() string {
    return "prints products in a table"
}

/* Flags declares --limit with a non-zero default: an unset flag reads back the declared value both under
the cli entry point and under melody:cron:run, whose dispatcher hands every scheduled command its declared
flags — the printed "product list: limit=…" line makes the honored default observable on a scheduled tick. */
func (instance *ProductListCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.IntFlag{
            Name:  productListFlagLimit,
            Usage: "maximum number of products to print (the declared default applies when the flag is not passed)",
            Value: productListDefaultLimit,
        },
    }
}

func (instance *ProductListCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    limit := int(commandContext.Int(productListFlagLimit))
    fmt.Printf("product list: limit=%d\n", limit)

    productService := service.MustGetProductService(runtimeInstance.Container())
    categoryService := service.MustGetCategoryService(runtimeInstance.Container())
    currencyService := service.MustGetCurrencyService(runtimeInstance.Container())

    products, listErr := productService.List()
    if nil != listErr {
        return listErr
    }

    if 0 < limit && limit < len(products) {
        products = products[:limit]
    }

    headers := []string{
        "ID",
        "NAME",
        "DESCRIPTION",
        "CATEGORY",
        "CURRENCY",
        "CREATED_AT",
        "UPDATED_AT",
    }

    rows := make([][]string, 0, len(products))

    for _, product := range products {
        if nil == product {
            continue
        }

        categoryName := "-"
        categoryId := product.CategoryId
        if "" != categoryId {
            category, _, categoryErr := categoryService.FindById(categoryId)
            if nil == categoryErr && nil != category {
                categoryName = category.Name
            }
        }

        currencyId := ""
        currencyName := "-"

        currencyId = product.CurrencyId

        if "" != currencyId {
            currency, _, currencyErr := currencyService.FindById(currencyId)
            if nil == currencyErr && nil != currency {
                currencyName = currency.Name
            }
        }

        rows = append(rows, []string{
            product.Id,
            product.Name,
            product.Description,
            categoryName + "(" + categoryId + ")",
            currencyName + "(" + currencyId + ")",
            product.CreatedAt.Format(time.DateTime),
            product.UpdatedAt.Format(time.DateTime),
        })
    }

    printTable(headers, rows)
    return nil
}

func printTable(headers []string, rows [][]string) {
    widths := make([]int, len(headers))
    for i, header := range headers {
        widths[i] = len(header)
    }

    for _, row := range rows {
        for i, col := range row {
            if len(col) > widths[i] {
                widths[i] = len(col)
            }
        }
    }

    printRow(headers, widths)
    printSeparator(widths)

    for _, row := range rows {
        printRow(row, widths)
    }
}

func printRow(columns []string, widths []int) {
    parts := make([]string, 0, len(columns))
    for i, column := range columns {
        padding := widths[i] - len(column)
        parts = append(parts, column+strings.Repeat(" ", padding))
    }

    fmt.Println(strings.Join(parts, "  |  "))
}

func printSeparator(widths []int) {
    parts := make([]string, 0, len(widths))
    for _, width := range widths {
        parts = append(parts, strings.Repeat("-", width))
    }

    fmt.Println(strings.Join(parts, "--+--"))
}

var _ melodyclicontract.Command = (*ProductListCommand)(nil)
