// Package sqlfixture provides an in-memory database for business regression tests.
// Production database connections never use this package.
package sqlfixture

import (
	"reflect"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Open creates a database for dialect-independent business fixtures.
func Open(dsn string, config *gorm.Config) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), config)
	if err != nil {
		return nil, err
	}
	// PostgreSQL defaults supply these non-primary sequence values in production.
	err = db.Callback().Create().Before("gorm:create").Register("fixture:sequence_ids", assignSequenceIDs)
	return db, err
}

func assignSequenceIDs(tx *gorm.DB) {
	if tx.Statement.Schema == nil {
		return
	}
	field := tx.Statement.Schema.LookUpField("SeqID")
	if field == nil {
		return
	}
	var maxID int64
	row := tx.Session(&gorm.Session{NewDB: true}).Table(tx.Statement.Table).Select("COALESCE(MAX(seq_id), 0)").Row()
	if err := row.Scan(&maxID); err != nil {
		tx.AddError(err)
		return
	}
	assign := func(value reflect.Value) {
		current, zero := field.ValueOf(tx.Statement.Context, value)
		if zero {
			maxID++
			tx.AddError(field.Set(tx.Statement.Context, value, maxID))
		} else if id, ok := current.(int64); ok && id > maxID {
			maxID = id
		}
	}
	value := tx.Statement.ReflectValue
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			assign(value.Index(i))
		}
	case reflect.Struct:
		assign(value)
	}
}
