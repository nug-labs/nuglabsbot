package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"telegram-v2/utils"

	"gopkg.in/yaml.v3"
)

type frontMatter struct {
	ID          string `yaml:"id"`
	Type        string `yaml:"type"`
	Audience    string `yaml:"audience"`
	CreatedAt   string `yaml:"created_at"`
	ScheduledAt string `yaml:"scheduled_at"`
}

func main() {
	loadLocalEnv()

	broadcastDir := filepath.Join(".", "assets", "broadcasts")
	entries, err := os.ReadDir(broadcastDir)
	if err != nil {
		panic(fmt.Errorf("read broadcasts dir: %w", err))
	}

	db, err := openDatabase()
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		path := filepath.Join(broadcastDir, name)
		if err := upsertBroadcastAndSeedOutgoing(ctx, db.SQL(), path); err != nil {
			panic(err)
		}
		loaded++
	}

	fmt.Printf("loaded %d broadcasts\n", loaded)
}

func openDatabase() (*utils.Database, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	raw, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := raw.PingContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}

	return utils.NewDatabase(raw), nil
}

func upsertBroadcastAndSeedOutgoing(ctx context.Context, db *sql.DB, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read broadcast file %s: %w", path, err)
	}

	parts := strings.Split(string(raw), "---")
	if len(parts) < 3 {
		return fmt.Errorf("invalid frontmatter format in %s", path)
	}

	var meta frontMatter
	if err := yaml.Unmarshal([]byte(parts[1]), &meta); err != nil {
		return fmt.Errorf("parse frontmatter %s: %w", path, err)
	}

	bodyYAML := strings.TrimSpace(strings.Join(parts[2:], "---"))
	var payload map[string]any
	if err := yaml.Unmarshal([]byte(bodyYAML), &payload); err != nil {
		return fmt.Errorf("parse body payload %s: %w", path, err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload %s: %w", path, err)
	}

	createdAt, err := time.Parse(time.RFC3339, meta.CreatedAt)
	if err != nil {
		return fmt.Errorf("parse created_at %s: %w", path, err)
	}

	var scheduledAt any
	if strings.TrimSpace(meta.ScheduledAt) != "" {
		t, err := time.Parse(time.RFC3339, meta.ScheduledAt)
		if err != nil {
			return fmt.Errorf("parse scheduled_at %s: %w", path, err)
		}
		scheduledAt = t
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for %q: %w", meta.ID, err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO broadcasts (id, type, payload, created_at)
		 VALUES ($1, $2, $3::jsonb, $4)
		 ON CONFLICT (id)
		 DO UPDATE SET
		   type = EXCLUDED.type,
		   payload = EXCLUDED.payload,
		   created_at = EXCLUDED.created_at`,
		meta.ID, meta.Type, string(payloadJSON), createdAt,
	)
	if err != nil {
		return fmt.Errorf("upsert broadcast %q: %w", meta.ID, err)
	}

	userFilter := "TRUE"
	switch strings.ToLower(strings.TrimSpace(meta.Audience)) {
	case "active_users":
		userFilter = "total_requests > 0"
	case "all":
		userFilter = "TRUE"
	}

	seedQuery := fmt.Sprintf(
		`INSERT INTO broadcast_outgoing (broadcast_id, user_id, scheduled_at, sent_time)
		 SELECT $1, u.telegram_id, $2, NULL
		 FROM users u
		 WHERE %s
		 ON CONFLICT (broadcast_id, user_id) DO NOTHING`,
		userFilter,
	)

	if _, err := tx.ExecContext(ctx, seedQuery, meta.ID, scheduledAt); err != nil {
		return fmt.Errorf("seed outgoing for %q: %w", meta.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx for %q: %w", meta.ID, err)
	}

	return nil
}

func loadLocalEnv() {
	if os.Getenv("DATABASE_URL") != "" {
		return
	}

	envPath := ".env"
	if strings.EqualFold(os.Getenv("APP_ENV"), "test") {
		envPath = ".env.test"
	}

	_ = loadEnvFile(envPath)
	if os.Getenv("DATABASE_URL") == "" && envPath != ".env" {
		_ = loadEnvFile(".env")
	}
}

func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}

	return scanner.Err()
}
