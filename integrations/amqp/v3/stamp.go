package amqp

import (
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const stampNameDelivery = "amqp_delivery"

type DeliveryStamp struct {
    Tag         uint64
    Redelivered bool

    Generation uint64
}

func (instance DeliveryStamp) StampName() string {
    return stampNameDelivery
}

var _ messagebuscontract.Stamp = DeliveryStamp{}
