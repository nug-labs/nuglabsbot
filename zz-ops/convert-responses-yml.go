package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"telegram-v2/utils"
)

func main() {
	utils.Env.InitOps()

	srcPath := filepath.Join(".", "assets", "responses.yml")
	entries, err := utils.ParseSimpleMapYAML(srcPath)
	if err != nil {
		panic(err)
	}

	db, err := utils.DatabaseManager.Init(context.Background())
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for key, message := range entries {
		_, err = db.ExecContext(
			ctx,
			`INSERT INTO responses (key, message)
			 VALUES ($1, $2)
			 ON CONFLICT (key)
			 DO UPDATE SET message = EXCLUDED.message, updated_at = NOW()`,
			key, message,
		)
		if err != nil {
			panic(fmt.Errorf("upsert response %q: %w", key, err))
		}
	}

	fmt.Printf("loaded %d responses\n", len(entries))
}
