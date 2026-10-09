// Command basic shows layered sources (defaults < files < env), profiles
// and expressions.
//
//	go run ./examples/basic
//	APP_PROFILE=dev go run ./examples/basic
//	APP_PROFILE=dev DB_HOST=env-db go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/format/json"
	"github.com/efuturetoday/gonf/format/properties"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/file"
	"github.com/efuturetoday/gonf/source/memory"
)

// DB is the configuration under "db". Its Mapping makes the keys known, so
// that DB_HOST and the other variables find them.
type DB struct {
	Host string
	URL  string
	Pool struct {
		MaxSize int
		Timeout time.Duration
	}
}

var dbMapping = gonf.NewMapping[DB]("db")

// exampleDir is the directory of this file, so the example finds its
// config files regardless of the working directory it is started from.
func exampleDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func main() {
	dir := exampleDir()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithProfiles(strings.Split(os.Getenv("APP_PROFILE"), ",")...),
		gonf.WithMappings(dbMapping),
		gonf.WithSources(
			memory.Defaults(map[string]string{
				"db.pool.max-size": "10",
				"db.pool.timeout":  "5s",
			}),
			file.New(filepath.Join(dir, "app.properties"), properties.Format),
			file.New(filepath.Join(dir, "app-dev.properties"), properties.Format,
				source.Optional(), source.WithProfile("dev")),
			file.New(filepath.Join(dir, "app.json"), json.Format, source.Optional()),
			env.New(),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, key := range []string{"db.url", "db.host", "db.pool.max-size", "db.pool.timeout"} {
		value, err := cfg.Get(key)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s = %s\n", key, value)
		for i, v := range cfg.Candidates(key) {
			marker := "  "
			if i == 0 {
				marker = "→ "
			}
			profile := ""
			if v.Origin.Profile != "" {
				profile = "%" + v.Origin.Profile + " "
			}
			fmt.Printf("  %s%-36q %sfrom %s (ordinal %d) %s\n",
				marker, v.Raw, profile, v.Origin.Source, v.Origin.Ordinal, v.Origin.Location)
		}
	}
}
