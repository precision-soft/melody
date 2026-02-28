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
