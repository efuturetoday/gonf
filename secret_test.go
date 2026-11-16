package gonf_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
)

// level parses itself, like any type with UnmarshalText.
type level int

func (l *level) UnmarshalText(b []byte) error {
	switch string(b) {
	case "low":
		*l = 1
	case "high":
		*l = 2
	default:
		return fmt.Errorf("unknown level %s", b)
	}
	return nil
}

type credentials struct {
	User     string
	Password gonf.Secret[string]
	Pin      gonf.Secret[int]      `default:"1234"`
	Level    gonf.Secret[level]    `default:"low"`
	Backup   *gonf.Secret[string]  // optional
	Keys     []gonf.Secret[string] `default:""`
	Named    map[string]gonf.Secret[string]
}

func TestSecret_Output(t *testing.T) {
	t.Parallel()
	s := gonf.NewSecret("hunter2")
	c := struct {
		User     string
		Password gonf.Secret[string]
	}{User: "u", Password: s}

	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "password", s, "config", c)
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		got  string
	}{
		{name: "string", got: s.String()},
		{name: "print", got: fmt.Sprint(s)},
		{name: "go syntax", got: fmt.Sprintf("%#v", s)},
		{name: "quoted", got: fmt.Sprintf("%q %x %d", s, s, s)},
		{name: "struct", got: fmt.Sprintf("%+v %#v", c, c)},
		{name: "pointer to struct", got: fmt.Sprintf("%v", &c)},
		{name: "json", got: string(encoded)},
		{name: "slog", got: logged.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if strings.Contains(tt.got, "hunter2") || !strings.Contains(tt.got, "****") {
				t.Errorf("output %q reveals the secret or does not mask it", tt.got)
			}
		})
	}
	if s.Value() != "hunter2" {
		t.Errorf("Value = %q", s.Value())
	}
	if s != gonf.NewSecret("hunter2") {
		t.Error("equal secrets compare unequal")
	}
}

func TestBind_Secret(t *testing.T) {
	t.Parallel()
	cfg := load(t, map[string]string{
		"app.user":     "u",
		"app.password": "hunter2",
		"app.level":    "high",
		"app.named.ci": "n1",
		"app.keys[0]":  "k0",
		"app.keys[1]":  "k1",
	}, nil)
	got, err := bind[credentials](cfg, "app")
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got.Keys))
	for _, k := range got.Keys {
		keys = append(keys, k.Value())
	}
	if got.User != "u" || got.Password.Value() != "hunter2" || got.Pin.Value() != 1234 || got.Level.Value() != 2 ||
		got.Backup != nil || strings.Join(keys, ",") != "k0,k1" || got.Named["ci"].Value() != "n1" {
		t.Errorf("Bind = user %q, password %q, pin %d, level %d, backup %v, keys %v, named %v",
			got.User, got.Password.Value(), got.Pin.Value(), got.Level.Value(), got.Backup, keys, got.Named["ci"].Value())
	}
}

func TestBind_SecretErrors(t *testing.T) {
	t.Parallel()
	type nested struct {
		Inner gonf.Secret[struct{ A string }]
	}
	tests := []struct {
		name          string
		bind          func(*gonf.Config) error
		expectedError string
	}{
		{
			name: "invalid value is hidden",
			bind: func(cfg *gonf.Config) error {
				_, err := bind[struct{ Pin gonf.Secret[int] }](cfg, "bad")
				return err
			},
			expectedError: `gonf: bind "bad": bad.pin: invalid int, value hidden (from file:app.properties)`,
		},
		{
			name: "error of UnmarshalText is hidden",
			bind: func(cfg *gonf.Config) error {
				_, err := bind[struct{ Level gonf.Secret[level] }](cfg, "worse")
				return err
			},
			expectedError: `gonf: bind "worse": worse.level: invalid gonf_test.level, value hidden (from file:app.properties)`,
		},
		{
			name: "missing secret is required",
			bind: func(cfg *gonf.Config) error {
				_, err := bind[struct{ Token gonf.Secret[string] }](cfg, "none")
				return err
			},
			expectedError: `gonf: bind "none": none.token: required, no value`,
		},
		{
			name: "secret of a struct",
			bind: func(cfg *gonf.Config) error {
				_, err := bind[nested](cfg, "none")
				return err
			},
			expectedError: `gonf: bind "none": none.inner: unsupported type gonf.Secret[struct { A string }], a secret must hold a single value`,
		},
		{
			name: "secret is not a struct to bind",
			bind: func(cfg *gonf.Config) error {
				_, err := bind[gonf.Secret[string]](cfg, "none")
				return err
			},
			expectedError: `gonf: bind "none": gonf.Secret[string] is not a struct`,
		},
	}
	cfg := load(t, map[string]string{"bad.pin": "12x4-hunter2", "worse.level": "hunter2"}, nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.bind(cfg)
			if err == nil || err.Error() != tt.expectedError {
				t.Fatalf("err = %v, want %q", err, tt.expectedError)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("error reveals the secret: %v", err)
			}
			if tt.name == "missing secret is required" && !errors.Is(err, gonf.ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestWithSecretWords(t *testing.T) {
	t.Parallel()
	values := map[string]string{"db.passphrase": "hunter2", "db.host": "h"}
	tests := []struct {
		name     string
		opts     []gonf.Option
		expected bool
	}{
		{name: "not secret by default"},
		{name: "added word", opts: []gonf.Option{gonf.WithSecretWords(" PassPhrase ", "")}, expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := load(t, values, nil, tt.opts...)
			e := cfg.Explain("db.passphrase")
			if e.IsSecret != tt.expected || (e.Value == "hunter2") == tt.expected {
				t.Errorf("IsSecret = %v, Value = %q, want IsSecret %v", e.IsSecret, e.Value, tt.expected)
			}
			if cfg.Explain("db.host").IsSecret {
				t.Error("db.host is secret")
			}
		})
	}
}

// A secret tag makes a plain field take a secret and masks it, like a
// Secret, but for keys that do not sound secret; a field without it still
// refuses the secret.
func TestBind_SecretTag(t *testing.T) {
	t.Parallel()
	type login struct {
		Key   string `secret:"true"`
		Cert  string `secret:"true" default:"pem"`
		Plain string `default:""`
	}
	values := map[string]string{"api.password": "s3cret", "api.key": "${api.password}"}
	m := gonf.NewMapping[login]("api")
	cfg := load(t, values, nil, gonf.WithMappings(m))
	if got := m.Get(cfg); got.Key != "s3cret" || got.Cert != "pem" {
		t.Errorf("Get = %+v", got)
	}
	if got, expected := cfg.Explain("api.key").String(), "api.key = **** (file:app.properties)"; got != expected {
		t.Errorf("Explain = %q, want %q", got, expected)
	}

	props, err := gonf.Describe(gonf.WithMappings(gonf.NewMapping[login]("api")))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range props {
		if want := p.Key != "api.plain"; p.Secret != want {
			t.Errorf("%s: Secret %v, want %v", p.Key, p.Secret, want)
		}
		if p.Key == "api.cert" && strings.Contains(fmt.Sprint(p.Default), "pem") {
			t.Errorf("api.cert shows its default %v", p.Default)
		}
	}

	values["api.plain"] = "${api.password}"
	_, err = bind[login](load(t, values, nil), "api")
	if err == nil || !strings.Contains(err.Error(), "api.plain: refers to the secret api.password") {
		t.Errorf("plain field: %v", err)
	}
}
