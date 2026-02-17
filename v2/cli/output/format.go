package output

type Format string

const (
	FormatTable Format = "table"
	FormatJson  Format = "json"
)

type SortOrder string

const (
	SortOrderAscending  SortOrder = "asc"
	SortOrderDescending SortOrder = "desc"
)
