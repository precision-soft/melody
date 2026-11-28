package output

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* ListPayload is one window of a list: the items shown, the total before the window, and the limit and offset as given, null when left out. */
type ListPayload[T any] struct {
    Items  []T         `json:"items"`
    Total  int         `json:"total"`
    Limit  bound.Count `json:"limit"`
    Offset bound.Count `json:"offset"`
}

/* ListPayloadConfig is the window a list payload reports beside its items; a limit or an offset left out is null. */
type ListPayloadConfig struct {
    Total  int
    Limit  bound.Count
    Offset bound.Count
}

func NewListPayload[T any](items []T, config ListPayloadConfig) ListPayload[T] {
    return ListPayload[T]{
        Items:  items,
        Total:  config.Total,
        Limit:  config.Limit,
        Offset: config.Offset,
    }
}

/* WindowItems answers the items from the offset on, at most limit of them, each resolved against the option's rule: an absent limit shows every item and an absent offset starts at the first. A limit or an offset the rule refuses is answered as the error, naming the field. */
func WindowItems[T any](
    items []T,
    limit bound.Count,
    offset bound.Count,
) ([]T, error) {
    resolvedLimit, limitErr := bound.Resolve(limit, OptionsLimitRule())
    if nil != limitErr {
        return nil, limitErr
    }

    resolvedOffset, offsetErr := bound.Resolve(offset, OptionsOffsetRule())
    if nil != offsetErr {
        return nil, offsetErr
    }

    total := len(items)

    startIndex := resolvedOffset.Value
    if total < startIndex {
        startIndex = total
    }

    endIndex := total
    /* startIndex+limit may overflow int, so the remaining count is compared instead of forming the sum */
    if false == resolvedLimit.Unlimited && resolvedLimit.Value < total-startIndex {
        endIndex = startIndex + resolvedLimit.Value
    }

    return items[startIndex:endIndex], nil
}

/* ReverseItems turns an ascending sort into a descending one in place. It runs before WindowItems so a descending --limit returns the tail of the ascending order rather than its head. */
func ReverseItems[T any](items []T) {
    leftIndex := 0
    rightIndex := len(items) - 1

    for leftIndex < rightIndex {
        items[leftIndex], items[rightIndex] = items[rightIndex], items[leftIndex]

        leftIndex = leftIndex + 1
        rightIndex = rightIndex - 1
    }
}

func ApplySortOrder[T any](items []T, order SortOrder) {
    if SortOrderDescending != order {
        return
    }

    ReverseItems(items)
}
