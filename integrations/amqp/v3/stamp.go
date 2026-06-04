package amqp

const stampNameDelivery = "amqp_delivery"

type DeliveryStamp struct {
    Tag         uint64
    Redelivered bool
}

func (instance DeliveryStamp) StampName() string {
    return stampNameDelivery
}
