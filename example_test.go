package gonf_test

import (
	"context"
	"fmt"
	"net/url"
	"time"

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

func ExampleBind() {
	type Pool struct {
		MaxSize int           `default:"10"`
		Idle    time.Duration `cfg:"idle-timeout" default:"5m"`
	}
	type DB struct {
		URL      string
		Pool     Pool
		Replicas []string
		Timeout  *time.Duration
	}
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(map[string]string{
		"db.host":          "localhost",
		"db.url":           "jdbc:postgresql://${db.host}/app",
		"db.pool.max-size": "20",
		"db.replicas":      "replica-1,replica-2",
	})))
	if err != nil {
		panic(err)
	}
	db, err := gonf.Bind[DB](cfg, "db")
	if err != nil {
		panic(err)
	}
	fmt.Println(db.URL)
	fmt.Println(db.Pool.MaxSize, db.Pool.Idle, db.Replicas, db.Timeout)
	// Output:
	// jdbc:postgresql://localhost/app
	// 20 5m0s [replica-1 replica-2] <nil>
}

func ExampleBindError() {
	type DB struct {
		URL     string
		MaxSize int
	}
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(
		map[string]string{"db.max-size": "ten", "db.ulr": "jdbc:postgresql://localhost/app"},
		source.WithName("file:app.properties"),
	)))
	if err != nil {
		panic(err)
	}
	_, err = gonf.Bind[DB](cfg, "db")
	fmt.Println(err)
	// Output:
	// gonf: bind "db": 3 problems:
	//   db.url: required, no value
	//   db.max-size: invalid int "ten" (from file:app.properties)
	//   db.ulr: unknown key, did you mean db.url? (from file:app.properties)
}

func ExampleMapping() {
	// In a library: the configuration of the module and its Mapping.
	type DBConfig struct {
		URL  string
		Pool struct {
			MaxSize int `default:"10"`
		}
	}
	db := gonf.NewMapping[DBConfig]("db")

	// In the application: Load binds and validates every Mapping.
	cfg, err := gonf.Load(context.Background(),
		gonf.WithMappings(db),
		gonf.WithSources(memory.New(map[string]string{"db.url": "jdbc:postgresql://localhost/app"})),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(db.Get(cfg).URL, db.Get(cfg).Pool.MaxSize)
	// Output: jdbc:postgresql://localhost/app 10
}

func ExampleSecret() {
	type DBConfig struct {
		User     string
		Password gonf.Secret[string]
	}
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(map[string]string{
		"db.user":     "app",
		"db.password": "hunter2",
	})))
	if err != nil {
		panic(err)
	}
	db, err := gonf.Bind[DBConfig](cfg, "db")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", db)
	fmt.Println(db.Password.Value())
	// Output:
	// {User:app Password:****}
	// hunter2
}

func ExampleConfig_Explanations() {
	type DBConfig struct {
		URL      string
		Password gonf.Secret[string]
		Pool     struct {
			MaxSize int `default:"10"`
		}
	}
	cfg, err := gonf.Load(context.Background(),
		gonf.WithMappings(gonf.NewMapping[DBConfig]("db")),
		gonf.WithSources(
			memory.New(map[string]string{
				"db.url":      "postgres://app:${db.password}@localhost/app",
				"db.password": "hunter2",
			}, source.WithName("file:app.properties"), source.WithOrdinal(100)),
			memory.New(map[string]string{
				"db.url": "postgres://app:${db.password}@prod/app",
			}, source.WithName("memory:overrides")),
		),
	)
	if err != nil {
		panic(err)
	}
	for _, e := range cfg.Explanations() {
		fmt.Println(e)
	}
	// Output:
	// db.password = **** (file:app.properties)
	// db.pool.max-size = 10 (mapping:db)
	// db.url = postgres://app:****@prod/app (memory:overrides)
	//   overrides postgres://app:${db.password}@localhost/app (file:app.properties)
}

func ExampleWithConverter() {
	type API struct {
		Endpoint *url.URL
	}
	cfg, err := gonf.Load(context.Background(),
		gonf.WithConverter(url.Parse),
		gonf.WithSources(memory.New(map[string]string{"api.endpoint": "https://example.com/v1"})),
	)
	if err != nil {
		panic(err)
	}
	api, err := gonf.Bind[API](cfg, "api")
	if err != nil {
		panic(err)
	}
	fmt.Println(api.Endpoint.Host, api.Endpoint.Path)
	// Output: example.com /v1
}
