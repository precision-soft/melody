package encrypt

/* CiphertextCandidatesFor answers the right-hand side of an equality lookup on the deterministic column of the marker R, reading the cipher, the binding and its switch from where the column reads them: for a marker without a binding, what its cipher's CiphertextCandidates answers; for a bound one, a gcm2 candidate per active key, the current key first, followed by the gcm1 candidates of the same keys while the binding admits unbound values, so a row not yet re-sealed still matches. Cipher.CiphertextCandidates answers gcm1 alone, and finds no row a bound column wrote. */
func CiphertextCandidatesFor[R CipherReference](plaintext string) ([][]byte, error) {
    cipherInstance, binding, resolveErr := referenceColumn[R]()
    if nil != resolveErr {
        return nil, resolveErr
    }

    if nil == binding {
        return cipherInstance.CiphertextCandidates(plaintext)
    }

    candidates, boundErr := cipherInstance.(bindingSealer).boundCandidates(plaintext, *binding)
    if nil != boundErr {
        return nil, boundErr
    }

    if true == binding.refuseUnbound {
        return candidates, nil
    }

    unbound, unboundErr := cipherInstance.CiphertextCandidates(plaintext)
    if nil != unboundErr {
        return nil, unboundErr
    }

    return append(candidates, unbound...), nil
}
