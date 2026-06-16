package config

import (
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

const encryptDemoKeyId = "example-2026"

func (instance *Module) buildEncrypt() {
    keyProvider := melodyencrypt.NewStaticKeyProvider(
        encryptDemoKeyId,
        map[string][]byte{
            encryptDemoKeyId: []byte("melody-example-demo-key-32-bytes"),
        },
    )

    cipher := melodyencrypt.NewCipher(keyProvider)
    melodyencrypt.UseCipher(cipher)

    instance.cipher = cipher
}
