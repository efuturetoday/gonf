package aesgcm_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/secret/aesgcm"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

// decrypt decrypts value with key through gonf.
func decrypt(t *testing.T, key, value string) (string, error) {
	t.Helper()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSecretHandler(aesgcm.Name, aesgcm.WithKey(key)),
		gonf.WithSources(memory.New(map[string]string{"v": "${aes-gcm::" + value + "}"})),
	)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Get("v")
}

func TestEncrypt(t *testing.T) {
	t.Parallel()
	first, err := aesgcm.Encrypt("k3y", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := aesgcm.Encrypt("k3y", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two encryptions are equal, want a new nonce each time")
	}
	for _, v := range []string{first, second} {
		if got, err := decrypt(t, "k3y", v); err != nil || got != "s3cret" {
			t.Errorf("decrypt(%q) = %q, %v; want s3cret", v, got, err)
		}
	}
}

func TestWithKey(t *testing.T) {
	t.Parallel()
	good, err := aesgcm.Encrypt("k3y", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	// Flip one bit of the ciphertext, behind the nonce length and nonce.
	raw, err := base64.RawURLEncoding.DecodeString(good)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	damaged := base64.RawURLEncoding.EncodeToString(raw)
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
		err      string
	}{
		// A value written by another implementation of this format.
		{name: "known value", key: "DDne5obnfH1RSeTg71xSZg", value: "DLTb_9zxThxeT5iAQqswEl5Dn1ju4FdM9hIyVip35t5V", expected: "1234"},
		{name: "padded base64", key: "k3y", value: good + "==", expected: "s3cret"},
		{name: "wrong key", key: "other", value: good, err: "aes-gcm: wrong key or damaged value"},
		{name: "damaged", key: "k3y", value: damaged, err: "aes-gcm: wrong key or damaged value"},
		{name: "not base64", key: "k3y", value: "not*base64", err: "aes-gcm: value is not URL-safe base64"},
		{name: "empty", key: "k3y", value: "", err: "aes-gcm: wrong key or damaged value"},
		{name: "zero nonce length", key: "k3y", value: "AAAA", err: "aes-gcm: wrong key or damaged value"},
		{name: "too short", key: "k3y", value: "DAAA", err: "aes-gcm: wrong key or damaged value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decrypt(t, tt.key, tt.value)
			if tt.err != "" {
				if err == nil || !strings.HasSuffix(err.Error(), tt.err) {
					t.Errorf("error = %v, want it to end in %q", err, tt.err)
				}
				return
			}
			if err != nil || got != tt.expected {
				t.Errorf("got %q, %v; want %q", got, err, tt.expected)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	t.Parallel()
	encrypted, err := aesgcm.Encrypt("k3y", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	type db struct {
		Password gonf.Secret[string]
	}
	file := map[string]string{"db.password": "${aes-gcm::" + encrypted + "}"}
	tests := []struct {
		name string
		file map[string]string
		env  map[string]string
		err  string
	}{
		{name: "key from another source", file: file, env: map[string]string{aesgcm.Key: "k3y"}},
		{
			name: "key next to the value",
			file: map[string]string{"db.password": file["db.password"], aesgcm.Key: "k3y"},
			err:  "the key gonf.secret.aes-gcm.key comes from the same source as the encrypted value (file:app.properties)",
		},
		{name: "no key", file: file, err: "the key gonf.secret.aes-gcm.key has no value"},
		{
			name: "key with an expression",
			file: file,
			env:  map[string]string{aesgcm.Key: "${other}"},
			err:  "the key gonf.secret.aes-gcm.key must be written as is, without ${...} (env:)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := gonf.Load(context.Background(),
				gonf.WithSecretHandler(aesgcm.Name, aesgcm.Handler),
				gonf.WithSources(
					memory.New(tt.file, source.WithName("file:app.properties"), source.WithOrdinal(100)),
					memory.New(tt.env, source.WithName("env:"), source.WithOrdinal(300)),
				),
			)
			if err != nil {
				t.Fatal(err)
			}
			got, err := gonf.Bind[db](cfg, "db", gonf.AllowUnknown())
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("error = %v, want it to contain %q", err, tt.err)
				}
				return
			}
			if err != nil || got.Password.Value() != "s3cret" {
				t.Errorf("got %q, %v; want s3cret", got.Password.Value(), err)
			}
		})
	}
}
