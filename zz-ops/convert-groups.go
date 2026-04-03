/*
convert-groups loads assets/groups.yml into the required_groups table.
Same pattern as convert-whitelist-yml / convert-broadcasts-yml: utils.Env.InitOps + DatabaseManager.Init.
Run from repo root: go run ./zz-ops/convert-groups.go (working directory app/telegram-v2).
*/
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"telegram-v2/utils"
)

type groupRow struct {
	ChatID     int64  `yaml:"chat_id"`
	Title      string `yaml:"title"`
	InviteLink string `yaml:"invite_link"`
	Enabled    bool   `yaml:"enabled"`
}

func main() {
	utils.Env.InitOps()

	path := filepath.Join(".", "assets", "groups.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Errorf("read groups yaml: %w", err))
	}
	var groups []groupRow
	if err := yaml.Unmarshal(raw, &groups); err != nil {
		panic(fmt.Errorf("parse groups yaml: %w", err))
	}

	db, err := utils.DatabaseManager.Init(context.Background())
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, g := range groups {
		if g.ChatID == 0 || g.InviteLink == "" {
			continue
		}
		_, err := db.ExecContext(
			ctx,
			`INSERT INTO required_groups (chat_id, title, invite_link, enabled, updated_at)
			 VALUES ($1, $2, $3, $4, NOW())
			 ON CONFLICT (chat_id)
			 DO UPDATE SET title = EXCLUDED.title, invite_link = EXCLUDED.invite_link, enabled = EXCLUDED.enabled, updated_at = NOW()`,
			g.ChatID, g.Title, g.InviteLink, g.Enabled,
		)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				panic(fmt.Errorf("upsert required_group %d: %w\n(hint: apply assets/db.sql — from app/telegram-v2: go run ./zz-ops/create-db.go)", g.ChatID, err))
			}
			panic(fmt.Errorf("upsert required_group %d: %w", g.ChatID, err))
		}
	}

	fmt.Printf("loaded %d groups\n", len(groups))
}
