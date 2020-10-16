package mysql

import (
	"github.com/juju/errors"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// DB ...
type DB struct {
	*gorm.DB
}

// Open ...
func Open(dsn string) (*DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return &DB{
		db,
	}, nil
}
