package amqp

import (
    "testing"

    amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestRedeliveryCountFromHeader(t *testing.T) {
    cases := []struct {
        name     string
        headers  amqp091.Table
        expected int
    }{
        {name: "missing", headers: amqp091.Table{}, expected: 0},
        {name: "int64", headers: amqp091.Table{headerRedeliveryCount: int64(3)}, expected: 3},
        {name: "int32", headers: amqp091.Table{headerRedeliveryCount: int32(2)}, expected: 2},
        {name: "int", headers: amqp091.Table{headerRedeliveryCount: 5}, expected: 5},
        {name: "wrong type", headers: amqp091.Table{headerRedeliveryCount: "7"}, expected: 0},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            got := redeliveryCountFromHeader(testCase.headers)
            if testCase.expected != got {
                t.Fatalf("expected %d, got %d", testCase.expected, got)
            }
        })
    }
}
