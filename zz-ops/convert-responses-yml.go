package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"telegram-v2/utils"
	"telegram-v2/utils/db"
)

func main() {
	utils.Env.InitOps()

	srcPath := filepath.Join(".", "assets", "responses.yml")
	entries, err := utils.ParseSimpleMapYAML(srcPath)
	if err != nil {
		panic(err)
	}

	database, err := db.DatabaseManager.Init(context.Background())
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for key, message := range entries {
		_, err = database.ExecContext(
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
