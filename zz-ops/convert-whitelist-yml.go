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

	srcPath := filepath.Join(".", "assets", "whitelist.yml")
	domains, err := parseWhitelistYAML(srcPath)
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

	for _, domain := range domains {
		_, err = db.ExecContext(
			ctx,
			`INSERT INTO whitelist (domain)
			 VALUES ($1)
			 ON CONFLICT (domain) DO NOTHING`,
			domain,
		)
		if err != nil {
			panic(fmt.Errorf("insert domain %q: %w", domain, err))
		}
	}

	fmt.Printf("loaded %d whitelist domains\n", len(domains))
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

func parseWhitelistYAML(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open whitelist yml: %w", err)
	}
	defer f.Close()

	var domains []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			raw = strings.Trim(raw, `"'`)
			if raw == "" {
				continue
			}
			domains = append(domains, normalizeWhitelistValue(raw))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan whitelist yml: %w", err)
	}
	return domains, nil
}

func normalizeWhitelistValue(raw string) string {
	return strings.TrimSpace(raw)
}
