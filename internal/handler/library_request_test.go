package handler

import (
	"reflect"
	"strings"
	"testing"

	"reliquary/internal/types"
)

// Library request and response types contain no vector binding fields.
func TestLibraryRequestsContainNoVectorBinding(t *testing.T) {
	t.Run("Library", func(t *testing.T) { assertNoVectorStoreIDField(t, reflect.TypeOf(types.Library{})) })
	t.Run("UpdateLibraryRequest", func(t *testing.T) {
		assertNoVectorStoreIDField(t, reflect.TypeOf(UpdateLibraryRequest{}))
	})
	t.Run("LibraryConfig", func(t *testing.T) {
		assertNoVectorStoreIDField(t, reflect.TypeOf(types.LibraryConfig{}))
	})
}

// assertNoVectorStoreIDField walks the visible fields of t (including embedded
// anonymous structs) and reports any field named VectorStoreID or carrying
// a json tag of "vector_store_id".
func assertNoVectorStoreIDField(t *testing.T, typ reflect.Type) {
	t.Helper()
	var visit func(rt reflect.Type, path string)
	visit = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			full := path + "." + f.Name
			if f.Name == "VectorStoreID" {
				t.Fatalf("%s declares VectorStoreID; "+
					"library DTOs must contain no vector binding", full)
			}
			if tag := strings.Split(f.Tag.Get("json"), ",")[0]; tag == "vector_store_id" {
				t.Fatalf("%s carries json tag \"vector_store_id\"; "+
					"post-create library DTOs must contain no vector binding", full)
			}
			if f.Anonymous {
				visit(f.Type, full)
			}
		}
	}
	visit(typ, typ.Name())
}
