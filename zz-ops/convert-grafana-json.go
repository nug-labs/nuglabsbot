/*
convert-grafana-json creates env-specific Grafana dashboards from template JSON files.
It uses utils.Env.InitOps() so zz-ops keeps the same interactive live/test prompt behavior.

Output files are written into app/nuglabsbot-v2 root:
  - live-grafana.json
  - test-grafana.json

Run from app/nuglabsbot-v2:

	go run ./zz-ops/convert-grafana-json.go
*/
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"nuglabsbot-v2/utils"
)

type dashboard struct {
	Title      string           `json:"title"`
	UID        string           `json:"uid"`
	Version    int              `json:"version"`
	Tags       []string         `json:"tags"`
	Templating map[string]any   `json:"templating"`
	Time       map[string]any   `json:"time"`
	Panels     []map[string]any `json:"panels"`
	Extras     map[string]any   `json:"-"`
}

func (d *dashboard) UnmarshalJSON(data []byte) error {
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	d.Extras = raw
	if v, ok := raw["title"].(string); ok {
		d.Title = v
	}
	if v, ok := raw["uid"].(string); ok {
		d.UID = v
	}
	if v, ok := raw["version"].(float64); ok {
		d.Version = int(v)
	}
	if v, ok := raw["tags"].([]any); ok {
		tags := make([]string, 0, len(v))
		for _, it := range v {
			if s, ok := it.(string); ok {
				tags = append(tags, s)
			}
		}
		d.Tags = tags
	}
	if v, ok := raw["templating"].(map[string]any); ok {
		d.Templating = v
	}
	if v, ok := raw["time"].(map[string]any); ok {
		d.Time = v
	}
	if v, ok := raw["panels"].([]any); ok {
		panels := make([]map[string]any, 0, len(v))
		for _, p := range v {
			if m, ok := p.(map[string]any); ok {
				panels = append(panels, m)
			}
		}
		d.Panels = panels
	}
	return nil
}

func (d dashboard) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range d.Extras {
		out[k] = v
	}
	out["title"] = d.Title
	out["uid"] = d.UID
	out["version"] = d.Version
	out["tags"] = d.Tags
	if d.Templating != nil {
		out["templating"] = d.Templating
	}
	if d.Time != nil {
		out["time"] = d.Time
	}
	if d.Panels != nil {
		out["panels"] = d.Panels
	}
	return json.Marshal(out)
}

func main() {
	utils.Env.InitOps()

	now := time.Now().UTC().Format(time.RFC3339)
	templatePath := filepath.Join(".", "grafana.json")

	type envConfig struct {
		name          string
		databaseURL   string
		datasourceUID string
	}

	cfgs := []envConfig{
		loadEnvConfig("live", ".env"),
		loadEnvConfig("test", ".env.test"),
	}

	for _, cfg := range cfgs {
		databaseLabel := summarizeDB(cfg.databaseURL)
		dst := filepath.Join(".", cfg.name+"-grafana.json")
		if err := convertOne(templatePath, dst, cfg.name, cfg.datasourceUID, databaseLabel, now); err != nil {
			panic(err)
		}
		fmt.Printf("wrote %s\n", dst)
	}
}

func convertOne(src, dst, appEnv, datasourceUID, databaseLabel, generatedAt string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	var d dashboard
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("parse %s: %w", src, err)
	}

	envPrefix := strings.ToUpper(appEnv[:1]) + appEnv[1:]
	d.Title = fmt.Sprintf("%s - %s", envPrefix, d.Title)
	d.UID = safeDashboardUID(appEnv + "-" + d.UID)
	d.Version = d.Version + 1
	d.Tags = appendUnique(d.Tags, "env:"+appEnv, "db:"+databaseLabel, "generated-by:zz-ops")

	applyDatasourceDefaults(&d, datasourceUID)
	if strings.TrimSpace(datasourceUID) != "" {
		replaceDatasourceUID(&d, datasourceUID)
	}

	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", dst, err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func applyDatasourceDefaults(d *dashboard, datasourceUID string) {
	_ = d
	_ = datasourceUID
}

func summarizeDB(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "unknown"
	}
	host := strings.TrimSpace(u.Hostname())
	dbName := strings.TrimPrefix(strings.TrimSpace(u.Path), "/")
	if host == "" && dbName == "" {
		return "unknown"
	}
	if dbName == "" {
		return host
	}
	if host == "" {
		return dbName
	}
	return host + "/" + dbName
}

func appendUnique(values []string, extras ...string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		seen[v] = struct{}{}
	}
	for _, ex := range extras {
		if strings.TrimSpace(ex) == "" {
			continue
		}
		if _, ok := seen[ex]; ok {
			continue
		}
		seen[ex] = struct{}{}
		values = append(values, ex)
	}
	return values
}

func loadEnvConfig(name, envFile string) struct {
	name          string
	databaseURL   string
	datasourceUID string
} {
	envs, _ := godotenv.Read(envFile)
	dbURL := strings.TrimSpace(envs["DATABASE_URL"])
	if dbURL == "" {
		// Fallback to current process env (already populated by InitOps) so generation still works.
		dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dbURL == "" {
		panic(fmt.Sprintf("DATABASE_URL missing for env=%s (file=%s)", name, envFile))
	}
	dsUID := strings.TrimSpace(envs["GRAFANA_POSTGRES_UID"])
	if dsUID == "" {
		dsUID = strings.TrimSpace(os.Getenv("GRAFANA_POSTGRES_UID"))
	}
	return struct {
		name          string
		databaseURL   string
		datasourceUID string
	}{
		name:          name,
		databaseURL:   dbURL,
		datasourceUID: dsUID,
	}
}

func replaceDatasourceUID(d *dashboard, datasourceUID string) {
	for k, v := range d.Extras {
		d.Extras[k] = replaceDatasourceUIDValue(v, datasourceUID)
	}
}

func replaceDatasourceUIDValue(v any, datasourceUID string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, nested := range t {
			// Common Grafana datasource object using template variables.
			if k == "uid" {
				if s, ok := nested.(string); ok && (s == "${DS_POSTGRES}" || s == "${DS_NUGLABS_PGQL}") {
					t[k] = datasourceUID
					continue
				}
			}
			t[k] = replaceDatasourceUIDValue(nested, datasourceUID)
		}
		return t
	case []any:
		for i := range t {
			t[i] = replaceDatasourceUIDValue(t[i], datasourceUID)
		}
		return t
	default:
		return v
	}
}

// safeDashboardUID enforces Grafana UID length <= 40.
// If the candidate is too long, keep a readable prefix and append a short hash suffix.
func safeDashboardUID(candidate string) string {
	const max = 40
	if len(candidate) <= max {
		return candidate
	}
	sum := sha1.Sum([]byte(candidate))
	sfx := hex.EncodeToString(sum[:])[:8]
	keep := max - 1 - len(sfx)
	if keep < 1 {
		return sfx
	}
	return candidate[:keep] + "-" + sfx
}
