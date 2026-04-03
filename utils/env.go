/*
Package utils/env handles environment bootstrapping for telegram-v2.
Injected from composition roots (app + zz-ops) before dependencies are built.
Workflow stage: process startup configuration loading.
*/
package utils

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type EnvManager struct{}

var Env = NewEnvManager()

func NewEnvManager() *EnvManager {
	return &EnvManager{}
}

// IsLive is true when APP_ENV is exactly "live" (case-insensitive). Background schedulers use this; unset/test/other values are non-live.
func (e *EnvManager) IsLive() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "live")
}

// Init loads .env files from the current working directory (godotenv does not override vars already set by the shell/Docker).
//   - APP_ENV unset or "test" → prefer .env.test, then .env if DATABASE_URL still empty
//   - any other APP_ENV (e.g. live, production) → .env
func (e *EnvManager) Init() {
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	switch appEnv {
	case "", "test":
		_ = godotenv.Load(".env.test")
		if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
			_ = godotenv.Load(".env")
		}
	default:
		_ = godotenv.Load(".env")
	}
}

// InitOps keeps existing shell values and only backfills from env files.
func (e *EnvManager) InitOps() {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) != "" {
		return
	}
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	switch appEnv {
	case "", "test":
		_ = readEnvFileNoOverride(".env.test")
		if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
			_ = readEnvFileNoOverride(".env")
		}
	default:
		_ = readEnvFileNoOverride(".env")
	}
}

func readEnvFileNoOverride(path string) error {
	envs, err := godotenv.Read(path)
	if err != nil {
		return err
	}
	for k, v := range envs {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			_ = os.Setenv(k, v)
		}
	}
	return nil
}
