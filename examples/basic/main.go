// Command basic shows layered sources: defaults < file < env < memory.
//
//	go run ./examples/basic
//	DB_URL=override go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/format/json"
	"github.com/efuturetoday/gonf/format/properties"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/file"
	"github.com/efuturetoday/gonf/source/memory"
)

// exampleDir is the directory of this file, so the example finds its
// config files regardless of the working directory it is started from.
func exampleDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func main() {
	dir := exampleDir()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(
		memory.Defaults(map[string]string{
			"db.pool.max-size": "10",
			"db.pool.timeout":  "5s",
		}),
		file.New(filepath.Join(dir, "application.properties"), properties.Format),
		file.New(filepath.Join(dir, "application.json"), json.Format, source.Optional()),
		env.New(),
	))
	if err != nil {
		log.Fatal(err)
	}

	for _, key := range []string{"db.url", "db.pool.max-size", "db.pool.timeout", "DB_URL"} {
		fmt.Println(key)
		for i, v := range cfg.Candidates(key) {
			marker := "  "
			if i == 0 {
				marker = "→ "
			}
			fmt.Printf("  %s%-20q from %s (ordinal %d) %s\n", marker, v.Raw, v.Origin.Source, v.Origin.Ordinal, v.Origin.Location)
		}
	}
}
