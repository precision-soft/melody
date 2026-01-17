package serializer

import (
	"testing"

	serializercontract "github.com/precision-soft/melody/serializer/contract"
)

type serializerTestSerializer struct {
	name string
}

func (instance *serializerTestSerializer) ContentType() string {
	return MimeApplicationJson + "; charset=utf-8"
}

func (instance *serializerTestSerializer) Serialize(value any) ([]byte, error) {
	return []byte(instance.name), nil
}

func (instance *serializerTestSerializer) Deserialize(payload []byte, target any) error {
	return nil
}

var _ serializercontract.Serializer = (*serializerTestSerializer)(nil)

func TestNewSerializerManager_PanicsOnEmptyMimeKey(t *testing.T) {
	_, err := NewSerializerManager(
		map[string]serializercontract.Serializer{
			"   ": &serializerTestSerializer{name: "x"},
		},
	)

	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestSerializerManager_Get_NormalizesMime(t *testing.T) {
	manager, err := NewSerializerManager(
		map[string]serializercontract.Serializer{
			"application/json": &serializerTestSerializer{name: "json"},
		},
	)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	serializerInstance, exists := manager.Get("application/json; charset=utf-8")
	if false == exists {
		t.Fatalf("expected serializer")
	}
	if nil == serializerInstance {
		t.Fatalf("expected non-nil serializer")
	}
}

func TestSerializerManager_ResolveByAcceptHeader_DefaultsToApplicationJson(t *testing.T) {
	manager, err := NewSerializerManager(
		map[string]serializercontract.Serializer{
			MimeApplicationJson: &serializerTestSerializer{name: "json"},
		},
	)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	serializerInstance, err := manager.ResolveByAcceptHeader("")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if nil == serializerInstance {
		t.Fatalf("expected serializer")
	}

	if MimeApplicationJson != normalizeMime(serializerInstance.ContentType()) {
		t.Fatalf("unexpected serializer content type: %s", serializerInstance.ContentType())
	}
}

type testSerializerPlain struct{}

func (instance *testSerializerPlain) ContentType() string {
	return "text/plain"
}

func (instance *testSerializerPlain) Serialize(payload any) ([]byte, error) {
	return []byte("plain"), nil
}

func (instance *testSerializerPlain) Deserialize(data []byte, target any) error {
	return nil
}

var _ serializercontract.Serializer = (*testSerializerPlain)(nil)

type testSerializerHtml struct{}

func (instance *testSerializerHtml) ContentType() string {
	return "text/html"
}

func (instance *testSerializerHtml) Serialize(payload any) ([]byte, error) {
	return []byte("html"), nil
}

func (instance *testSerializerHtml) Deserialize(data []byte, target any) error {
	return nil
}

var _ serializercontract.Serializer = (*testSerializerHtml)(nil)

func TestSerializerManager_ResolveByAcceptHeader_WildcardSubtype_SelectsLexicalFirst(t *testing.T) {
	manager, err := NewSerializerManager(
		map[string]serializercontract.Serializer{
			"text/plain": &testSerializerPlain{},
			"text/html":  &testSerializerHtml{},
		},
	)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	resolved, err := manager.ResolveByAcceptHeader("text/*")
	if nil != err {
		t.Fatalf("unexpected error")
	}

	if "text/html" != normalizeMime(resolved.ContentType()) {
		t.Fatalf("expected lexical first content type to win for wildcard subtype")
	}
}
