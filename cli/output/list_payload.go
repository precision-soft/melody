package output

type ListPayload[T any] struct {
    Items  []T `json:"items"`
    Total  int `json:"total"`
    Limit  int `json:"limit"`
    Offset int `json:"offset"`
}

func NewListPayload[T any](
    items []T,
    total int,
    limit int,
    offset int,
) ListPayload[T] {
    return ListPayload[T]{
        Items:  items,
        Total:  total,
        Limit:  limit,
        Offset: offset,
    }
}

func WindowItems[T any](
    items []T,
    limit int,
    offset int,
) []T {
    total := len(items)

    startIndex := offset
    if 0 > startIndex {
        startIndex = 0
    }
    if total < startIndex {
        startIndex = total
    }

    endIndex := total
    if 0 < limit {
        /* startIndex+limit overflows int for a large limit and wraps negative, so the slice bound below panics; compare against the remaining count instead of forming the sum */
        if limit < total-startIndex {
            endIndex = startIndex + limit
        }
    }

    return items[startIndex:endIndex]
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
