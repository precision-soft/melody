package entity

func NewCategory(id string, name string) *Category {
    return &Category{
        Id:   id,
        Name: name,
    }
}

type Category struct {
    Id   string
    Name string
}
