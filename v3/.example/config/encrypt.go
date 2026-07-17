package config

import (
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    bun "github.com/uptrace/bun"
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

/* encryptDatabaseFactory resolves the shared *bun.DB for the melody:encrypt:database bulk command; the
encrypt module evaluates it at the first command run — after Boot — so the database is never opened for the
command's sake in http or worker mode, and a boot without a configured database still registers the command
(the first run then reports the missing service). */
func (instance *Module) encryptDatabaseFactory(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
    return melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
}
