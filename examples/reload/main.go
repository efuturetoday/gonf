// Command reload shows a configuration that stays current. Run it from
// this directory and edit config/app.properties while it runs:
//
//	cd examples/reload
//	go run .
//
// A valid change shows up within a few seconds; an invalid one, such as
// app.interval=soon, is logged and the previous configuration stays.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
)

// App is the configuration under "app".
type App struct {
	Greeting string        `default:"hello"`
	Interval time.Duration `default:"1s"`
}

var appConfig = gonf.NewMapping[App]("app")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	live, err := boot.Watch(ctx, boot.WithMappings(appConfig))
	if err != nil {
		log.Fatal(err)
	}
	for {
		// Read the current Config for every unit of work.
		app := appConfig.Get(live.Config())
		log.Println(app.Greeting)
		select {
		case <-ctx.Done():
			return
		case <-time.After(app.Interval):
		}
	}
}
