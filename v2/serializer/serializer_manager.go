package serializer

import (
	"sort"
	"strings"

	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
)

func NewSerializerManager(serializersByMime map[string]serializercontract.Serializer) (*SerializerManager, error) {
	if nil == serializersByMime {
		serializersByMime = map[string]serializercontract.Serializer{}
	}

	normalizedSerializersByMime := make(map[string]serializercontract.Serializer, len(serializersByMime))
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

		if nil == serializerInstance {
			return nil, exception.NewError(
				"serializer instance is nil",
				exceptioncontract.Context{
					"mime": normalizedMimeKey,
				},
				nil,
			)
		}

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

func (instance *SerializerManager) ResolveByAcceptHeader(acceptHeader string) (serializercontract.Serializer, error) {
	acceptHeader = strings.TrimSpace(acceptHeader)
	if "" == acceptHeader {
		serializerInstance, exists := instance.serializersByMime[MimeApplicationJson]
		if true == exists {
			return serializerInstance, nil
		}

		return nil, exception.NewError("no default serializer configured", nil, nil)
	}

	acceptedMimes := parseAcceptHeader(acceptHeader)
	if 0 == len(acceptedMimes) {
		return nil, exception.NewError(
			"no acceptable mime types in accept header",
			exceptioncontract.Context{"accept": acceptHeader},
			nil,
		)
	}

	for _, acceptedMimeValue := range acceptedMimes {
		if "*/*" == acceptedMimeValue.mime {
			serializerInstance, exists := instance.serializersByMime[MimeApplicationJson]
			if true == exists {
				return serializerInstance, nil
			}
			continue
		}

		serializerInstance, exists := instance.serializersByMime[acceptedMimeValue.mime]
		if true == exists {
			return serializerInstance, nil
		}

		if true == isWildcardSubtype(acceptedMimeValue.mime) {
			candidateMimes := make([]string, 0, len(instance.serializersByMime))
			for candidateMime := range instance.serializersByMime {
				candidateMimes = append(candidateMimes, candidateMime)
			}

			sort.Strings(candidateMimes)

			for _, candidateMime := range candidateMimes {
				candidateSerializer := instance.serializersByMime[candidateMime]
				if true == matchWildcardSubtype(acceptedMimeValue.mime, candidateMime) {
					return candidateSerializer, nil
				}
			}
		}
	}

	serializerInstance, exists := instance.serializersByMime[MimeApplicationJson]
	if true == exists {
		return serializerInstance, nil
	}

	return nil, exception.NewError("no serializer found for accept header", exceptioncontract.Context{"accept": acceptHeader}, nil)
}
