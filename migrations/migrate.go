package migrations

import (
	"embed"
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var scripts embed.FS

func Up(dsn string) error {
	source, err := iofs.New(scripts, ".")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		_ = source.Close()
		return err
	}
	err = m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		err = nil
	}
	sourceErr, databaseErr := m.Close()
	return errors.Join(err, sourceErr, databaseErr)
}
