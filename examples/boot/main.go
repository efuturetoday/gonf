// Command boot shows loading by convention: embedded defaults, config/,
// .env and environment variables, with the profile from GONF_PROFILE.
//
// Run it from this directory, because config/ and .env are looked up in
// the working directory:
//
//	cd examples/boot
//	go run .
//	GONF_PROFILE= go run .
//	DB_HOST=env-db go run .
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
)

// DB is the configuration under "db". In a real application it lives in
// the package that uses it, next to its Mapping.
type DB struct {
	URL  string
	Pool struct {
		MaxSize int `default:"10"`
	}
	Timeout time.Duration `default:"5s"`
}

var dbMapping = gonf.NewMapping[DB]("db")

//go:embed embedded
var embedded embed.FS

func main() {
	defaults, err := fs.Sub(embedded, "embedded")
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := boot.Load(context.Background(),
		boot.WithFS(defaults),
		boot.With(gonf.WithMappings(dbMapping)),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("profiles: %v\n", cfg.Profiles())
	for _, key := range []string{"db.url", "db.pool.max-size"} {
		value, err := cfg.Get(key)
		if err != nil {
			log.Fatal(err)
		}
		v, _ := cfg.Lookup(key)
		fmt.Printf("%s = %s (from %s, ordinal %d)\n", key, value, v.Origin.Source, v.Origin.Ordinal)
	}

	fmt.Printf("bound: %+v\n", dbMapping.Get(cfg))
}
