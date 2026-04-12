/*
convert-whitelist-yml loads assets/whitelist.yml via utils.ParseSimpleListYAML,
connects with utils.Env.InitOps + db.DatabaseManager.Init, and upserts domains into whitelist.
Stand-alone zz-ops entrypoint (not the Telegram binary).
*/
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

func main() {
	utils.Env.InitOps()

	srcPath := filepath.Join(".", "assets", "whitelist.yml")
	domains, err := utils.ParseSimpleListYAML(srcPath)
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

	for _, domain := range domains {
		_, err = database.ExecContext(
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
