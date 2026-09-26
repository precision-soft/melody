package cli

import (
    "fmt"
    "io"
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
    writer := commandContext.Writer()

    limit := int(commandContext.Int(productListFlagLimit))
    _, _ = fmt.Fprintf(writer, "product list: limit=%d\n", limit)

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

    /* both nomenclatures are read once and answered from a map rather than looked up per product and per column; a name missing between the two reads renders as "-". */
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

    return fprintTable(writer, headers, rows)
}

/* nameById reads a whole nomenclature once and keys its names by identifier. A read that fails answers an empty map beside its error, so every name renders as a dash, the listing keeps rendering, and the caller journals the loss. */
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

/* nameOrDash answers a dash for an identifier it cannot resolve and for the empty identifier of a product filed under none. */
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

/* fprintTable renders into the command's own writer and answers the first write that failed, so a table the operator never received does not exit zero. The widths are counted in runes, not bytes: a multi-byte name padded by its byte length shifts every separator to its right and misaligns the table. */
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

    if rowErr := printRow(writer, headers, widths); nil != rowErr {
        return rowErr
    }
    if separatorErr := printSeparator(writer, widths); nil != separatorErr {
        return separatorErr
    }

    for _, row := range rows {
        if rowErr := printRow(writer, row, widths); nil != rowErr {
            return rowErr
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
