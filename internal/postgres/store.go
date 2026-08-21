package postgres

import (
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the concrete PostgreSQL adapter for Carry's current fact owners.
type Store struct {
	pool    *pgxpool.Pool
	queries *dbsqlc.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: dbsqlc.New(pool)}
}
