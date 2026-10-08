package yaml_test

import (
	"context"
	"fmt"
	"log"
	"testing/fstest"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
	"github.com/efuturetoday/gonf/format/yaml"
)

func ExampleFormat() {
	type DB struct {
		URL  string
		Pool struct{ MaxSize int }
	}
	db := gonf.NewMapping[DB]("db")

	// In an application, app.yaml is embedded with //go:embed.
	embedded := fstest.MapFS{"app.yaml": {Data: []byte(`
db:
  url: postgres://localhost/app
  pool:
    max-size: 10
`)}}
	cfg, err := boot.Load(context.Background(),
		boot.WithFormat("yaml", yaml.Format),
		boot.WithFS(embedded),
		boot.WithMappings(db),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", db.Get(cfg))
	fmt.Println(cfg.Explain("db.pool.max-size"))
	// Output:
	// {URL:postgres://localhost/app Pool:{MaxSize:10}}
	// db.pool.max-size = 10 (fs:app.yaml:5)
}
