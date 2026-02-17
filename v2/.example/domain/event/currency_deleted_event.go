package event

const (
	CurrencyDeletedEventName = "currency.deleted"
)

func NewCurrencyDeletedEvent(currencyId string) *CurrencyDeletedEvent {
	return &CurrencyDeletedEvent{currencyId: currencyId}
}

type CurrencyDeletedEvent struct {
	currencyId string
}

func (instance *CurrencyDeletedEvent) CurrencyId() string {
	return instance.currencyId
}
