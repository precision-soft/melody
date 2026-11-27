package amqp

import (
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

const stampNameDelivery = "amqp_delivery"

type DeliveryStamp struct {
    Tag         uint64
    Redelivered bool

    Generation uint64

    /* what the delivery arrived with, so its requeue is never refused for a size the broker already carried */
    receivedHeaderCount int
    receivedHeaderBytes int
}

func (instance DeliveryStamp) StampName() string {
    return stampNameDelivery
}

var _ messagebuscontract.Stamp = DeliveryStamp{}
