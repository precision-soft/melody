package cli

import (
    "fmt"
    "io"
    "strings"
    "time"
    "unicode/utf8"

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

/* Flags declares --limit with a non-zero default: an unset flag reads back the declared value both under the cli entry point and under melody:cron:run, whose dispatcher hands every scheduled command its declared flags — the printed "product list: limit=…" line makes the honored default observable on a scheduled tick. A non-positive value prints every product, the same convention the framework's own melody:outbox:relay --limit follows. */
func (instance *ProductListCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.IntFlag{
            Name:  productListFlagLimit,
            Usage: "maximum number of products to print; 0 prints them all (the declared default applies when the flag is not passed)",
            Value: productListDefaultLimit,
        },
    }
}

func (instance *ProductListCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    limit := int(commandContext.Int(productListFlagLimit))
    writer := commandContext.Writer()
    if _, writeErr := fmt.Fprintf(writer, "product list: limit=%d\n", limit); nil != writeErr {
        return writeErr
    }

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
            if nil != categoryErr {
                return categoryErr
            }
            if nil != category {
                categoryName = category.Name
            }
        }

        currencyId := product.CurrencyId
        currencyName := "-"

        if "" != currencyId {
            currency, _, currencyErr := currencyService.FindById(currencyId)
            if nil != currencyErr {
                return currencyErr
            }
            if nil != currency {
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

    return fprintTable(writer, headers, rows)
}

func fprintTable(writer io.Writer, headers []string, rows [][]string) error {
    widths := make([]int, len(headers))
    for i, header := range headers {
        widths[i] = utf8.RuneCountInString(header)
    }

    for _, row := range rows {
        for i, col := range row {
            if utf8.RuneCountInString(col) > widths[i] {
                widths[i] = utf8.RuneCountInString(col)
            }
        }
    }

    if writeErr := printRow(writer, headers, widths); nil != writeErr {
        return writeErr
    }
    if writeErr := printSeparator(writer, widths); nil != writeErr {
        return writeErr
    }

    for _, row := range rows {
        if writeErr := printRow(writer, row, widths); nil != writeErr {
            return writeErr
        }
    }

    return nil
}

func printRow(writer io.Writer, columns []string, widths []int) error {
    parts := make([]string, 0, len(columns))
    for i, column := range columns {
        padding := widths[i] - utf8.RuneCountInString(column)
        parts = append(parts, column+strings.Repeat(" ", padding))
    }

    _, writeErr := fmt.Fprintln(writer, strings.Join(parts, "  |  "))
    return writeErr
}

func printSeparator(writer io.Writer, widths []int) error {
    parts := make([]string, 0, len(widths))
    for _, width := range widths {
        parts = append(parts, strings.Repeat("-", width))
    }

    _, writeErr := fmt.Fprintln(writer, strings.Join(parts, "--+--"))
    return writeErr
}

var _ melodyclicontract.Command = (*ProductListCommand)(nil)
