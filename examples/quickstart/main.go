// Command quickstart is the example of the README. Run it from this
// directory, because config/ is looked up in the working directory:
//
//	cd examples/quickstart
//	go run .
//	APP_PORT=9000 go run .
//	GONF_PROFILE=dev go run .
//	go run . app.db.url
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
)

type Config struct {
	Port int `default:"8080" desc:"HTTP port"`
	DB   struct {
		Host     string              `default:"localhost" desc:"database host"`
		URL      string              `desc:"connection string"`
		Password gonf.Secret[string] `default:"change-me"`
	}
}

var appConfig = gonf.NewMapping[Config]("app")

func main() {
	log.SetFlags(0)
	cfg, err := boot.Load(context.Background(), boot.WithMappings(appConfig))
	if err != nil {
		log.Fatal(err)
	}

	for _, w := range cfg.Warnings() {
		log.Println("warning:", w)
	}

	c := appConfig.Get(cfg)
	fmt.Println("port:    ", c.Port)
	fmt.Println("db.url:  ", c.DB.URL)
	fmt.Println("password:", c.DB.Password)

	// go run . app.db.url
	if len(os.Args) > 1 {
		fmt.Println(cfg.Explain(os.Args[1]))
	}
}
