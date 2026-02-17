package entity

import "time"

func NewProduct(
	id string,
	name string,
	description string,
	categoryId string,
	price float64,
	currencyId string,
	stock int64,
	createdAt time.Time,
	updatedAt time.Time,
) *Product {
	return &Product{
		Id:          id,
		Name:        name,
		Description: description,
		CategoryId:  categoryId,
		Price:       price,
		CurrencyId:  currencyId,
		Stock:       stock,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

type Product struct {
	Id          string
	Name        string
	Description string
	CategoryId  string
	Price       float64
	CurrencyId  string
	Stock       int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
