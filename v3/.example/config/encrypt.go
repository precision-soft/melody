package config

import (
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    bun "github.com/uptrace/bun"
)

const encryptCompartmentKeyId = "example-2026"

func (instance *Module) buildEncrypt() {
    keyProvider := melodyencrypt.NewStaticKeyProvider(
        encryptCompartmentKeyId,
        map[string][]byte{
            encryptCompartmentKeyId: []byte("melody-example-cipher-key-32byte"),
        },
    )

    cipher := melodyencrypt.NewCipher(keyProvider)
    melodyencrypt.UseCipher(cipher)

    instance.cipher = cipher
}

/* encryptDatabaseFactory resolves the shared *bun.DB for the melody:encrypt:database bulk command at its first run, after Boot, so http and worker processes never open the database for it and a boot without a database still registers the command. */
func (instance *Module) encryptDatabaseFactory(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
    return melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
}
