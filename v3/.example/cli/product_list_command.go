package cli

import (
    "fmt"
    "io"
    "os"
    "strings"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

    /* both nomenclatures are read ONCE and answered from a map, where the listing
    used to ask FindById per product and per column: a page of N products cost 2N
    lookups to render two columns whose whole vocabulary is two short tables. A
    name that has gone missing between the two reads still renders as "-", which
    is what the per-product lookup answered for it. */
    categoryNameById, categoryListErr := nameById(categoryService.List, func(category *entity.Category) (string, string) {
        return category.Id, category.Name
    })
    journalLostNomenclature(runtimeInstance, "category", categoryListErr)

    currencyNameById, currencyListErr := nameById(currencyService.List, func(currency *entity.Currency) (string, string) {
        return currency.Id, currency.Name
    })
    journalLostNomenclature(runtimeInstance, "currency", currencyListErr)

    rows := make([][]string, 0, len(products))

    for _, product := range products {
        if nil == product {
            continue
        }

        categoryId := product.CategoryId
        categoryName := nameOrDash(categoryNameById, categoryId)

        currencyId := product.CurrencyId
        currencyName := nameOrDash(currencyNameById, currencyId)

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

/* nameById reads a whole nomenclature once and keys its names by identifier. A
read that fails answers an empty map beside its error, so every name renders as
the dash the per-product lookup rendered when ITS read failed: the listing keeps
rendering, and the caller journals the loss. */
func nameById[Entity any](list func() ([]*Entity, error), identify func(*Entity) (string, string)) (map[string]string, error) {
    entityList, listErr := list()
    if nil != listErr {
        return map[string]string{}, listErr
    }

    nameById := make(map[string]string, len(entityList))
    for _, entityInstance := range entityList {
        if nil == entityInstance {
            continue
        }

        identifier, name := identify(entityInstance)
        nameById[identifier] = name
    }

    return nameById, nil
}

/* journalLostNomenclature records a nomenclature the listing could not read. The
listing does not refuse over it — the products are what was asked for, and a column
of dashes is still a listing — but a column of dashes printed in silence reads as
products filed under nothing, so the loss goes to the journal with its cause. */
func journalLostNomenclature(runtimeInstance melodyruntimecontract.Runtime, nomenclature string, listErr error) {
    if nil == listErr {
        return
    }

    examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
        "product listing rendered a nomenclature it could not read as dashes",
        melodyexception.LogContext(listErr, melodyloggingcontract.Context{"nomenclature": nomenclature}),
    )
}

/* nameOrDash answers the dash the per-product lookup answered for an identifier
it could not resolve, and for the empty identifier of a product filed under none. */
func nameOrDash(nameById map[string]string, identifier string) string {
    if "" == identifier {
        return "-"
    }

    name, exists := nameById[identifier]
    if false == exists {
        return "-"
    }

    return name
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
