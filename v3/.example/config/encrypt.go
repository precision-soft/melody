package config

import (
    "strings"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    bun "github.com/uptrace/bun"
)

/* the development key the example encrypts under when APP_ENCRYPT_KEYS names none, so a checkout boots without a key of its own */
const (
    encryptDevelopmentKeyId = "example-2026"
    encryptDevelopmentKey   = "melody-example-cipher-key-32byte"
)

/* buildEncrypt builds the cipher over the keys APP_ENCRYPT_KEYS names, each written id:key and separated by commas, and encrypts under APP_ENCRYPT_CURRENT_KEY. Every named key decrypts, so a key is retired by adding the next, making it current, re-encrypting with melody:encrypt:database --mode reencrypt --target-key <next>, and only then removing the key it replaced. */
func (instance *Module) buildEncrypt() {
    currentKeyId, keysById := encryptKeys(
        instance.environmentValue(environmentKeyEncryptKeys),
        instance.environmentValue(environmentKeyEncryptCurrentKey),
    )

    cipher := melodyencrypt.NewCipher(melodyencrypt.NewStaticKeyProvider(currentKeyId, keysById))
    melodyencrypt.UseCipher(cipher)

    instance.cipher = cipher
}

/* encryptKeys reads the key list and the current key id. An entry that is not id:key panics the boot naming the entry's position and id, never the key, since a malformed key list is a wiring mistake and a boot that skipped it would fail to decrypt what it encrypted. Without a list, the development key is the one key. */
func encryptKeys(rawKeyList string, currentKeyId string) (string, map[string][]byte) {
    if "" == strings.TrimSpace(rawKeyList) {
        return encryptDevelopmentKeyId, map[string][]byte{encryptDevelopmentKeyId: []byte(encryptDevelopmentKey)}
    }

    keysById := make(map[string][]byte)
    for position, entry := range strings.Split(rawKeyList, ",") {
        keyId, key, found := strings.Cut(strings.TrimSpace(entry), ":")
        if false == found || "" == keyId || "" == key {
            melodyexception.Panic(melodyexception.NewError(
                environmentKeyEncryptKeys+" entry is not written id:key",
                melodyexceptioncontract.Context{"position": position + 1, "keyId": keyId},
                nil,
            ))
        }

        if _, duplicated := keysById[keyId]; true == duplicated {
            melodyexception.Panic(melodyexception.NewError(
                environmentKeyEncryptKeys+" names a key id twice",
                melodyexceptioncontract.Context{"position": position + 1, "keyId": keyId},
                nil,
            ))
        }

        keysById[keyId] = []byte(key)
    }

    return currentKeyId, keysById
}

/* encryptDatabaseFactory resolves the shared *bun.DB for the melody:encrypt:database bulk command at its first run, after Boot, so http and worker processes never open the database for it and a boot without a database still registers the command. */
func (instance *Module) encryptDatabaseFactory(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
    return melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
}
