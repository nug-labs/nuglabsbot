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

	srcPath := filepath.Join(".", "assets", "responses.yml")
	entries, err := parseSimpleMapYAML(srcPath)
	if err != nil {
		panic(err)
	}

	db, err := openDatabase()
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

func parseSimpleMapYAML(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open responses yml: %w", err)
	}
	defer f.Close()

	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		k := strings.Trim(strings.TrimSpace(parts[0]), `"'`)
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if k != "" {
			out[k] = v
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan responses yml: %w", err)
	}
	return out, nil
}
