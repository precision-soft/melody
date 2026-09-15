package service

import (
    "context"
    "time"

    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
)

const absenceCacheTtl = time.Minute

func rememberEntityOrAbsence(
    cacheInstance melodycachecontract.Cache,
    cacheKey string,
    loader func(ctx context.Context) (any, error),
) (any, error) {
    cached, exists, getErr := cacheInstance.Get(cacheKey)
    if nil == getErr && true == exists {
        return cached, nil
    }

    computedHere := false

    computed, rememberErr := melodycache.Remember(
        cacheInstance,
        cacheKey,
        absenceCacheTtl,
        func(ctx context.Context) (any, error) {
            computedHere = true

            return loader(ctx)
        },
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    if nil == computed || false == computedHere {
        return computed, nil
    }

    setErr := cacheInstance.Set(cacheKey, computed, entityCacheTtl)
    if nil != setErr {
        return nil, setErr
    }

    return computed, nil
}

const entityCacheTtl = time.Duration(0)
