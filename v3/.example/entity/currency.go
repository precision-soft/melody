package entity

func NewCurrency(
    id string,
    code string,
    name string,
) *Currency {
    return &Currency{
        Id:   id,
        Code: code,
        Name: name,
    }
}

type Currency struct {
    Id   string
    Code string
    Name string
}
