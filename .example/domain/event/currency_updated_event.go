package event

import "github.com/precision-soft/melody/.example/domain/entity"

const (
	CurrencyUpdatedEventName = "currency.updated"
)

func NewCurrencyUpdatedEvent(currency *entity.Currency) *CurrencyUpdatedEvent {
	return &CurrencyUpdatedEvent{currency: currency}
}

type CurrencyUpdatedEvent struct {
	currency *entity.Currency
}

func (instance *CurrencyUpdatedEvent) Currency() *entity.Currency {
	return instance.currency
}
