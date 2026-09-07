package cli

import (
    "fmt"
    "io"
    "os"
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

/* printTable renders to standard output, which is where the commands that only ever print a table want it.
   A command whose output a test reads passes its own writer through fprintTable instead: the command
   context carries one for exactly that reason, and capturing a process stream to assert a table is a test
   about plumbing rather than about the command. */
func printTable(headers []string, rows [][]string) {
    fprintTable(os.Stdout, headers, rows)
}

/* the widths are measured in RUNES, not bytes: a multi-byte name padded by its byte length shifts every separator to its right and misaligns the whole table — the frozen majors' examples left this class behind when they moved onto the framework's table builder */
func fprintTable(writer io.Writer, headers []string, rows [][]string) {
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

    printRow(writer, headers, widths)
    printSeparator(writer, widths)

    for _, row := range rows {
        printRow(writer, row, widths)
    }
}

func printRow(writer io.Writer, columns []string, widths []int) {
    parts := make([]string, 0, len(columns))
    for i, column := range columns {
        padding := widths[i] - utf8.RuneCountInString(column)
        parts = append(parts, column+strings.Repeat(" ", padding))
    }

    _, _ = fmt.Fprintln(writer, strings.Join(parts, "  |  "))
}

func printSeparator(writer io.Writer, widths []int) {
    parts := make([]string, 0, len(widths))
    for _, width := range widths {
        parts = append(parts, strings.Repeat("-", width))
    }

    _, _ = fmt.Fprintln(writer, strings.Join(parts, "--+--"))
}

var _ melodyclicontract.Command = (*ProductListCommand)(nil)
