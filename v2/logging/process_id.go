package logging

import (
	"crypto/rand"
	"fmt"

	"github.com/precision-soft/melody/v2/exception"
)

func GenerateProcessId() string {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if nil != err {
		exception.Panic(exception.NewError("failed to generate process id", nil, err))
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	)
}
