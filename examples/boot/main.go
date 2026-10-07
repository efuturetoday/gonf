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

// DB is bound to the keys under "db".
type DB struct {
	URL  string
	Pool struct {
		MaxSize int `default:"10"`
	}
	Timeout time.Duration `default:"5s"`
}

//go:embed embedded
var embedded embed.FS

func main() {
	defaults, err := fs.Sub(embedded, "embedded")
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := boot.Load(context.Background(), boot.WithFS(defaults))
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

	db, err := gonf.Bind[DB](cfg, "db")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("bound: %+v\n", db)
}
