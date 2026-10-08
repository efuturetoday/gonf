// Command library-module shows a library that brings its own
// configuration (package db) and an application that loads it and prints
// its configuration reference.
//
// Run it from this directory, because config/ is looked up in the working
// directory:
//
//	cd examples/library-module
//	GONF_PROFILE=dev go run .
//	DB_PASSWORD=secret go run .
//	go run .                  # fails: db.password is required
//	go run . -config-doc      # prints the keys as a Markdown table
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
	"github.com/efuturetoday/gonf/examples/library-module/db"
)

func main() {
	configDoc := flag.Bool("config-doc", false, "print the configuration keys and exit")
	flag.Parse()
	// The same options document and load the configuration.
	opts := []boot.Option{boot.WithMappings(db.Mapping)}

	if *configDoc {
		props, err := boot.Describe(opts...)
		if err != nil {
			log.Fatal(err)
		}
		if err := gonf.WriteMarkdown(os.Stdout, props); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := boot.Load(context.Background(), opts...)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(db.Open(cfg))
	fmt.Println(cfg.Explain("db.password"))
}
