package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return pool, nil
}

// Migrate owns the migration ledger bootstrap. Its small SQL statements are
// intentionally outside sqlc because the application schema may not exist yet.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	const migrationLockID int64 = 724_637_441
	if _, err := connection.Exec(ctx, `select pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		_, _ = connection.Exec(context.Background(), `select pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := connection.Exec(ctx, `
		create table if not exists carry_schema_migrations (
			version text primary key,
			applied_at timestamptz not null default transaction_timestamp()
		)
	`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	filenames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			filenames = append(filenames, entry.Name())
		}
	}
	sort.Strings(filenames)

	for _, filename := range filenames {
		var applied bool
		if err := connection.QueryRow(ctx,
			`select exists(select 1 from carry_schema_migrations where version = $1)`,
			filename,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", filename, err)
		}
		if applied {
			continue
		}

		migration, err := migrationFiles.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}
		if err := applyMigration(ctx, connection, filename, string(migration)); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, connection *pgxpool.Conn, filename string, migration string) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", filename, err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	if _, err := transaction.Exec(ctx, migration); err != nil {
		return fmt.Errorf("apply migration %s: %w", filename, err)
	}
	if _, err := transaction.Exec(ctx,
		`insert into carry_schema_migrations (version) values ($1)`,
		filename,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", filename, err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filename, err)
	}
	return nil
}
