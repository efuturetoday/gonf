// Package db stands for a library that brings its own configuration: it
// declares the keys it reads under "db", with defaults and descriptions,
// and the application only registers the Mapping.
package db

import (
	"fmt"
	"time"

	"github.com/efuturetoday/gonf"
)

// Config is the configuration under "db".
type Config struct {
	URL      string              `desc:"Address of the database, such as postgres://localhost/app."`
	User     string              `default:"app" desc:"User to log in as."`
	Password gonf.Secret[string] `desc:"Password of the user."`
	Pool     struct {
		MaxSize int           `default:"10" desc:"Maximum number of open connections."`
		Timeout time.Duration `default:"5s" desc:"How long to wait for a free connection."`
	}
}

// Validate checks the values that the types cannot.
func (c *Config) Validate() error {
	if c.Pool.MaxSize < 1 {
		return fmt.Errorf("pool.max-size must be at least 1, got %d", c.Pool.MaxSize)
	}
	return nil
}

// Mapping is passed to gonf.WithMappings by the application.
var Mapping = gonf.NewMapping[Config]("db")

// Open pretends to connect to the database.
func Open(cfg *gonf.Config) string {
	c := Mapping.Get(cfg)
	return fmt.Sprintf("connected to %s as %s with password %v, pool of %d", c.URL, c.User, c.Password, c.Pool.MaxSize)
}
