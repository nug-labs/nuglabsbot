package utils

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const dbPingTimeout = 5 * time.Second

func OpenDatabaseFromEnv(ctx context.Context) (*Database, error) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	raw, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := raw.PingContext(pingCtx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return NewDatabase(raw), nil
}
