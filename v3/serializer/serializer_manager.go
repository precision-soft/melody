package serializer

import (
    "errors"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

/* ErrNotAcceptable reports that the accept header refused every media type the manager can produce, so no representation can be served. */
var ErrNotAcceptable = errors.New("no acceptable media type for the accept header")

const notAcceptableMessage = "the accept header refuses every available media type"

func NewSerializerManager(serializersByMime map[string]serializercontract.Serializer) (*SerializerManager, error) {
    if nil == serializersByMime {
        serializersByMime = map[string]serializercontract.Serializer{}
    }

    normalizedSerializersByMime := make(map[string]serializercontract.Serializer, len(serializersByMime))
    rawKeysByNormalizedMime := make(map[string]string, len(serializersByMime))
    for mimeKey, serializerInstance := range serializersByMime {
        normalizedMimeKey := normalizeMime(mimeKey)
        if "" == normalizedMimeKey {
            return nil, exception.NewError(
                "serializer mime key is empty",
                exceptioncontract.Context{
                    "mime": mimeKey,
                },
                nil,
            )
        }

        /* a typed nil is refused with the untyped one, at construction rather than on the request path */
        if true == internal.IsNilInterface(serializerInstance) {
            return nil, exception.NewError(
                "serializer instance is nil",
                exceptioncontract.Context{
                    "mime": normalizedMimeKey,
                },
                nil,
            )
        }

        /* two spellings normalizing to one key are refused, since map order would pick the survivor */
        occupiedRawKey, occupied := rawKeysByNormalizedMime[normalizedMimeKey]
        if true == occupied {
            conflictingKeys := []string{occupiedRawKey, mimeKey}
            sort.Strings(conflictingKeys)

            return nil, exception.NewError(
                "serializer mime keys collide after normalization",
                exceptioncontract.Context{
                    "mime":         normalizedMimeKey,
                    "firstMimeKey": conflictingKeys[0],
                    "otherMimeKey": conflictingKeys[1],
                },
                nil,
            )
        }

        rawKeysByNormalizedMime[normalizedMimeKey] = mimeKey
        normalizedSerializersByMime[normalizedMimeKey] = serializerInstance
    }

    return &SerializerManager{
        serializersByMime: normalizedSerializersByMime,
    }, nil
}

type SerializerManager struct {
    serializersByMime map[string]serializercontract.Serializer
}

func (instance *SerializerManager) Get(mime string) (serializercontract.Serializer, bool) {
    normalizedMime := normalizeMime(mime)
    if "" == normalizedMime {
        return nil, false
    }

    serializerInstance, exists := instance.serializersByMime[normalizedMime]
    if false == exists {
        return nil, false
    }

    return serializerInstance, true
}

/* defaultSerializer answers the representation served when the header expresses no usable preference: json when registered, otherwise the first serializer in lexical mime order. */
func (instance *SerializerManager) defaultSerializer() (serializercontract.Serializer, bool) {
    return instance.defaultSerializerExcluding(nil)
}

/* defaultSerializerExcluding is defaultSerializer with the types the header refused with q=0 left out. */
func (instance *SerializerManager) defaultSerializerExcluding(refusedMimes map[string]struct{}) (serializercontract.Serializer, bool) {
    if _, refused := refusedMimes[MimeApplicationJson]; false == refused {
        serializerInstance, exists := instance.serializersByMime[MimeApplicationJson]
        if true == exists {
            return serializerInstance, true
        }
    }

    configuredMimes := make([]string, 0, len(instance.serializersByMime))
    for configuredMime := range instance.serializersByMime {
        if _, refused := refusedMimes[configuredMime]; true == refused {
            continue
        }

        configuredMimes = append(configuredMimes, configuredMime)
    }

    if 0 == len(configuredMimes) {
        return nil, false
    }

    sort.Strings(configuredMimes)

    return instance.serializersByMime[configuredMimes[0]], true
}

func (instance *SerializerManager) ResolveByAcceptHeader(acceptHeader string) (serializercontract.Serializer, error) {
    acceptHeader = strings.TrimSpace(acceptHeader)
    if "" == acceptHeader {
        serializerInstance, exists := instance.defaultSerializer()
        if true == exists {
            return serializerInstance, nil
        }

        return nil, exception.NewError("no default serializer configured", nil, nil)
    }

    acceptedMimes, cut := parseAcceptHeader(acceptHeader)
    if true == cut {
        return nil, exception.NewError(
            "the accept header holds more members than the negotiation reads and is refused whole",
            exceptioncontract.Context{"accept": acceptHeader},
            ErrNotAcceptable,
        )
    }

    if 0 == len(acceptedMimes) {
        return nil, exception.NewError(
            "no acceptable mime types in accept header",
            exceptioncontract.Context{"accept": acceptHeader},
            nil,
        )
    }

    candidateMimes := make([]string, 0, len(instance.serializersByMime))
    for candidateMime := range instance.serializersByMime {
        candidateMimes = append(candidateMimes, candidateMime)
    }

    sort.Strings(candidateMimes)

    /* each type takes the quality of the most specific range covering it, so an exact range overrides a wildcard; a type refused with q=0 is never served, and not acceptable is answered only when every type is refused */
    selectedMime := ""
    selectedQuality := 0.0
    selectedSpecificity := 0
    refusedMimes := map[string]struct{}{}

    for _, candidateMime := range candidateMimes {
        quality, specificity, matched := acceptQualityFor(acceptedMimes, candidateMime)
        if false == matched {
            continue
        }

        if 0 == quality {
            refusedMimes[candidateMime] = struct{}{}

            continue
        }

        if quality > selectedQuality {
            selectedMime = candidateMime
            selectedQuality = quality
            selectedSpecificity = specificity

            continue
        }

        if quality == selectedQuality && specificity > selectedSpecificity {
            selectedMime = candidateMime
            selectedSpecificity = specificity

            continue
        }

        /* a full tie resolves json first, as the empty header does; other ties keep the lexically first candidate */
        if quality == selectedQuality && specificity == selectedSpecificity && MimeApplicationJson == candidateMime {
            selectedMime = candidateMime
        }
    }

    if "" != selectedMime {
        return instance.serializersByMime[selectedMime], nil
    }

    if len(refusedMimes) == len(candidateMimes) && 0 < len(refusedMimes) {
        return nil, exception.NewError(
            notAcceptableMessage,
            exceptioncontract.Context{"accept": acceptHeader},
            ErrNotAcceptable,
        )
    }

    serializerInstance, exists := instance.defaultSerializerExcluding(refusedMimes)
    if true == exists {
        return serializerInstance, nil
    }

    return nil, exception.NewError("no serializer found for accept header", exceptioncontract.Context{"accept": acceptHeader}, nil)
}
