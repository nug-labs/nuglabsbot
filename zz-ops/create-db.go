package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"telegram-v2/utils"
)

func main() {
	utils.Env.InitOps()

	schemaPath := filepath.Join(".", "assets", "db.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		panic(fmt.Errorf("read schema file: %w", err))
	}

	db, err := utils.DatabaseManager.Init(context.Background())
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		panic(fmt.Errorf("apply schema: %w", err))
	}

	fmt.Println("schema applied successfully")
}
