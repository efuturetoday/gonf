package gonf_test

import (
	"context"
	"fmt"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

func ExampleLoad() {
	cfg, err := gonf.Load(context.Background(),
		gonf.WithProfiles("dev"),
		gonf.WithSources(
			memory.Defaults(map[string]string{"db.pool.max-size": "10"}),
			memory.New(map[string]string{
				"db.url":           "jdbc:postgresql://prod-db/app",
				"%dev.db.url":      "jdbc:postgresql://localhost/app",
				"db.pool.max-size": "20",
			}, source.WithName("file:application.properties"), source.WithOrdinal(100)),
		),
	)
	if err != nil {
		panic(err)
	}
	for _, key := range cfg.Keys() {
		v, _ := cfg.Lookup(key)
		fmt.Printf("%s = %s (from %s, profile %q)\n", key, v.Raw, v.Origin.Source, v.Origin.Profile)
	}
	// Output:
	// db.pool.max-size = 20 (from file:application.properties, profile "")
	// db.url = jdbc:postgresql://localhost/app (from file:application.properties, profile "dev")
}

func ExampleConfig_Get() {
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(map[string]string{
		"db.host": "localhost",
		"db.url":  "jdbc:postgresql://${db.host}:${db.port:5432}/app",
	})))
	if err != nil {
		panic(err)
	}
	url, err := cfg.Get("db.url")
	if err != nil {
		panic(err)
	}
	fmt.Println(url)
	// Output: jdbc:postgresql://localhost:5432/app
}

func ExampleWithFactories() {
	// A factory creates Sources that need configuration themselves, here a
	// stand-in for a Vault Source whose address comes from a file.
	vault := func(_ context.Context, cfg *gonf.Config) ([]gonf.Source, error) {
		addr, err := cfg.Get("vault.addr")
		if err != nil {
			return nil, fmt.Errorf("vault: %w", err)
		}
		return []gonf.Source{memory.New(
			map[string]string{"db.password": "s3cret"},
			source.WithName("vault:"+addr),
		)}, nil
	}
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"vault.addr": "https://vault:8200"})),
		gonf.WithFactories(vault),
	)
	if err != nil {
		panic(err)
	}
	v, _ := cfg.Lookup("db.password")
	fmt.Println(v.Origin.Source)
	// Output: vault:https://vault:8200
}
