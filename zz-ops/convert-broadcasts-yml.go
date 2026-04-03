package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"telegram-v2/utils"
)

type frontMatter struct {
	ID          string `yaml:"id"`
	Type        string `yaml:"type"`
	Audience    string `yaml:"audience"`
	CreatedAt   string `yaml:"created_at"`
	ScheduledAt string `yaml:"scheduled_at"`
}

func main() {
	utils.Env.InitOps()

	broadcastDir := filepath.Join(".", "assets", "broadcasts")
	entries, err := os.ReadDir(broadcastDir)
	if err != nil {
		panic(fmt.Errorf("read broadcasts dir: %w", err))
	}

	db, err := utils.DatabaseManager.Init(context.Background())
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
		if err := upsertBroadcastAndSeedOutgoing(ctx, db, path); err != nil {
			panic(err)
		}
		loaded++
	}

	fmt.Printf("loaded %d broadcasts\n", loaded)
}

func upsertBroadcastAndSeedOutgoing(ctx context.Context, db *utils.Database, path string) error {
	var meta frontMatter
	var payload map[string]any
	if err := utils.ParseFrontMatterYAML(path, &meta, &payload); err != nil {
		return err
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

	tx, err := db.SQL().BeginTx(ctx, nil)
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
