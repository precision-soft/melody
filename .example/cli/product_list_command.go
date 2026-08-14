package cli

import (
    "fmt"
    "time"

    "github.com/precision-soft/melody/.example/service"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyoutput "github.com/precision-soft/melody/cli/output"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

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

func (instance *ProductListCommand) Flags() []melodyclicontract.Flag {
    return melodyoutput.StandardFlags()
}

type productListItem struct {
    Id          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description"`
    Category    string `json:"category"`
    Currency    string `json:"currency"`
    CreatedAt   string `json:"createdAt"`
    UpdatedAt   string `json:"updatedAt"`
}

func (instance *ProductListCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    startedAt := time.Now()

    option := melodyoutput.NormalizeOption(
        melodyoutput.ParseOptionFromCommand(commandContext),
    )

    meta := melodyoutput.NewMeta(
        instance.Name(),
        commandContext.Args().Slice(),
        option,
        startedAt,
        time.Duration(0),
        melodyoutput.Version{},
    )

    envelope := melodyoutput.NewEnvelope(meta)

    productService := service.MustGetProductService(runtimeInstance.Container())
    categoryService := service.MustGetCategoryService(runtimeInstance.Container())
    currencyService := service.MustGetCurrencyService(runtimeInstance.Container())

    products, listErr := productService.List()
    if nil != listErr {
        return listErr
    }

    items := make([]productListItem, 0, len(products))

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

        currencyName := "-"
        currencyId := product.CurrencyId
        if "" != currencyId {
            currency, _, currencyErr := currencyService.FindById(currencyId)
            if nil == currencyErr && nil != currency {
                currencyName = currency.Name
            }
        }

        items = append(items, productListItem{
            Id:          product.Id,
            Name:        product.Name,
            Description: product.Description,
            Category:    categoryName + "(" + categoryId + ")",
            Currency:    currencyName + "(" + currencyId + ")",
            CreatedAt:   product.CreatedAt.Format(time.DateTime),
            UpdatedAt:   product.UpdatedAt.Format(time.DateTime),
        })
    }

    /* the service answers oldest first, so the ascending default renders the catalogue in its creation order */
    melodyoutput.ApplySortOrder(items, option.Order)

    total := len(items)
    items = melodyoutput.WindowItems(items, option.Limit, option.Offset)

    if melodyoutput.FormatTable == option.Format {
        builder := melodyoutput.NewTableBuilder()

        summary := fmt.Sprintf("PRODUCTS: %d total", total)
        if len(items) != total {
            summary = fmt.Sprintf("%s | %d shown", summary, len(items))
        }
        builder.AddSummaryLine(summary)

        block := builder.AddBlock(
            "PRODUCTS",
            []string{"ID", "NAME", "DESCRIPTION", "CATEGORY", "CURRENCY", "CREATED_AT", "UPDATED_AT"},
        )

        for _, item := range items {
            block.AddRow(
                item.Id,
                item.Name,
                item.Description,
                item.Category,
                item.Currency,
                item.CreatedAt,
                item.UpdatedAt,
            )
        }

        envelope.Table = builder.Build()
    } else {
        envelope.Data = melodyoutput.NewListPayload(
            items,
            total,
            option.Limit,
            option.Offset,
        )
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return melodyoutput.Render(commandContext.Writer, envelope, option)
}

var _ melodyclicontract.Command = (*ProductListCommand)(nil)
