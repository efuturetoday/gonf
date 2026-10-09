package flagsource_test

import (
	"context"
	"flag"
	"fmt"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
	"github.com/efuturetoday/gonf/source/flagsource"
)

func Example() {
	type DB struct {
		URL      string `default:"localhost" desc:"Database address."`
		Password string `desc:"Login password."`
	}
	mapping := gonf.NewMapping[DB]("db")
	opts := []boot.Option{boot.WithMappings(mapping)}

	// One flag per key, with help text.
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	props, err := boot.Describe(opts...)
	if err != nil {
		panic(err)
	}
	if err := flagsource.Define(fs, props); err != nil {
		panic(err)
	}
	// boot does not parse; the application does.
	if err := fs.Parse([]string{"-db.url=postgres://flag/app", "-db.password=s3cret"}); err != nil {
		panic(err)
	}

	cfg, err := boot.Load(context.Background(), append(opts, boot.WithFlags(fs))...)
	if err != nil {
		panic(err)
	}
	fmt.Println(mapping.Get(cfg).URL)
	// Output:
	// postgres://flag/app
}
