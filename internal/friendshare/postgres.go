package friendshare

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// This small service stores its bounded state in one row. Row locks serialize mutations
// across service instances, and polling reads a committed snapshot without locking writers.
type postgresStore struct {
	db *sql.DB
}

func OpenPostgres(ctx context.Context, dsn, setup string) (http.Handler, func(), error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, nil, errors.New("invalid DATABASE_URL")
	}
	// Avoid relying on a persistent server-side prepared statement with pooled URLs.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(2 * time.Minute)
	store := &postgresStore{db: db}
	s, err := newService(setup, store)
	if err == nil {
		err = store.init(ctx)
	}
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return s, func() { _ = db.Close() }, nil
}

func (p *postgresStore) init(ctx context.Context) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(716928240)"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS elsewhere_friends_state (
		id integer PRIMARY KEY CHECK (id = 1),
		payload jsonb NOT NULL
	)`); err != nil {
		return err
	}
	data, err := json.Marshal(emptyDatabase())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO elsewhere_friends_state VALUES (1, $1::jsonb) ON CONFLICT DO NOTHING", string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *postgresStore) ready(ctx context.Context) error {
	var id int
	return p.db.QueryRowContext(ctx, "SELECT id FROM elsewhere_friends_state WHERE id = 1").Scan(&id)
}

func (p *postgresStore) apply(ctx context.Context, write bool, change stateChange) (Reply, int, error) {
	const query = "SELECT payload FROM elsewhere_friends_state WHERE id = 1"
	var raw []byte
	if !write {
		if err := p.db.QueryRowContext(ctx, query).Scan(&raw); err != nil {
			return Reply{}, 0, err
		}
		state, err := decodeDatabase(raw)
		if err != nil {
			return Reply{}, 0, err
		}
		_, reply, code := change(state)
		return reply, code, nil
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return Reply{}, 0, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, query+" FOR UPDATE").Scan(&raw); err != nil {
		return Reply{}, 0, err
	}
	state, err := decodeDatabase(raw)
	if err != nil {
		return Reply{}, 0, err
	}
	next, reply, code := change(state)
	if next == nil {
		return reply, code, nil
	}
	data, err := json.Marshal(next)
	if err != nil {
		return Reply{}, 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE elsewhere_friends_state SET payload = $1::jsonb WHERE id = 1", string(data)); err != nil {
		return Reply{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return Reply{}, 0, err
	}
	return reply, code, nil
}
