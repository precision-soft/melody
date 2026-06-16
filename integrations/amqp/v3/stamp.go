package amqp

const stampNameDelivery = "amqp_delivery"

type DeliveryStamp struct {
    Tag         uint64
    Redelivered bool

    Generation uint64
}

func (instance DeliveryStamp) StampName() string {
    return stampNameDelivery
}
