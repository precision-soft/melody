package event

import "github.com/precision-soft/melody/.example/domain/entity"

const (
	CurrencyCreatedEventName = "currency.created"
)

func NewCurrencyCreatedEvent(currency *entity.Currency) *CurrencyCreatedEvent {
	return &CurrencyCreatedEvent{currency: currency}
}

type CurrencyCreatedEvent struct {
	currency *entity.Currency
}

func (instance *CurrencyCreatedEvent) Currency() *entity.Currency {
	return instance.currency
}
