package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"telegram-v2/utils"
)

func main() {
	loadLocalEnv()

	schemaPath := filepath.Join(".", "assets", "db.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		panic(fmt.Errorf("read schema file: %w", err))
	}

	db, err := openDatabase()
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

func loadLocalEnv() {
	// Do not override shell-provided env.
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
