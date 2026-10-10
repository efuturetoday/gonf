// Package aesgcm decrypts configuration values encrypted with AES-256 in GCM
// mode, written as ${aes-gcm::value}:
//
//	db.password=${aes-gcm::DBJ2...}
//
// Register Function, and Mapping for its key, with gonf.WithFunctions and
// gonf.WithMappings; boot does that on its own. Encrypt creates such
// values.
//
// value is URL-safe base64, with or without padding, of one byte holding
// the nonce length, the nonce, and the ciphertext with its 16-byte tag. The
// AES key is the SHA-256 hash of the key string.
package aesgcm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/efuturetoday/gonf"
)

const (
	// Name is the name of the Function in ${aes-gcm::value}.
	Name = "aes-gcm"
	// Key is the configuration key that holds the key for Function, found
	// in the environment as GONF_SECRET_AES_GCM_KEY.
	Key = "gonf.secret.aes-gcm.key"
)

// KeyConfig holds the key for Function.
type KeyConfig struct {
	Key *gonf.Secret[string] `desc:"Key for ${aes-gcm::...} values."`
}

// Mapping binds KeyConfig to the prefix of Key. Registered with Function,
// it makes Key a known key, so the environment provides it as
// GONF_SECRET_AES_GCM_KEY. boot registers it with its default Functions.
var Mapping = gonf.NewMapping[KeyConfig](strings.TrimSuffix(Key, ".key"))

// nonceSize is the nonce length Encrypt uses, the standard one of GCM.
const nonceSize = 12

var errDecrypt = errors.New("wrong key or damaged value")

// Function returns the secret gonf.Function Name that decrypts with the
// key the configuration holds under Key. Register Mapping with it so that
// the environment can provide the key. Any Source may provide the key,
// except the Source of the encrypted value: a key next to the values it
// protects protects nothing. The key is used as written; it must not
// contain ${...}.
//
// gonf caches results by value, not by Source: a value decrypted once is
// not checked again where it appears in the Source of the key.
func Function() gonf.Function {
	return gonf.Function{Name: Name, IsSecret: true, Call: decryptWithConfig}
}

// decryptWithConfig decrypts value with the key under Key.
func decryptWithConfig(cfg *gonf.Config, value string, origin gonf.Origin) (string, error) {
	key, ok := cfg.Lookup(Key)
	switch {
	case !ok || key.Raw == "":
		return "", fmt.Errorf("the key %s has no value", Key)
	case strings.Contains(key.Raw, "${"):
		return "", fmt.Errorf("the key %s must be written as is, without ${...} (%s)", Key, key.Origin.Source)
	case key.Origin.Source == origin.Source:
		return "", fmt.Errorf("the key %s comes from the same source as the encrypted value (%s)", Key, origin.Source)
	}
	return decrypt(key.Raw, value)
}

// WithKey returns the secret gonf.Function Name that decrypts with key,
// for applications that get the key elsewhere.
func WithKey(key string) gonf.Function {
	return gonf.Function{Name: Name, IsSecret: true, Call: func(_ *gonf.Config, value string, _ gonf.Origin) (string, error) {
		return decrypt(key, value)
	}}
}

// Encrypt encrypts plaintext with key and returns the value for a
// ${aes-gcm::value} expression. Every call uses a new random nonce, so the
// same plaintext gives a different value each time.
func Encrypt(key, plaintext string) (string, error) {
	aead, err := newAEAD(key, nonceSize)
	if err != nil {
		return "", err
	}
	out := make([]byte, 1+nonceSize, 1+nonceSize+len(plaintext)+aead.Overhead())
	out[0] = nonceSize
	if _, err := rand.Read(out[1:]); err != nil {
		return "", fmt.Errorf("aesgcm: nonce: %w", err)
	}
	out = aead.Seal(out, out[1:], []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// decrypt decrypts value, as written by Encrypt, with key. Its errors never
// contain the plaintext.
func decrypt(key, value string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		return "", errors.New("value is not URL-safe base64")
	}
	if len(data) == 0 || data[0] == 0 || len(data) < 1+int(data[0]) {
		return "", errDecrypt
	}
	n := int(data[0])
	aead, err := newAEAD(key, n)
	if err != nil {
		return "", err
	}
	plain, err := aead.Open(nil, data[1:1+n], data[1+n:], nil)
	if err != nil {
		return "", errDecrypt
	}
	return string(plain), nil
}

// newAEAD returns AES-256-GCM with the SHA-256 hash of key and the given
// nonce size.
func newAEAD(key string, nonceSize int) (cipher.AEAD, error) {
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("aesgcm: %w", err)
	}
	aead, err := cipher.NewGCMWithNonceSize(block, nonceSize)
	if err != nil {
		return nil, fmt.Errorf("aesgcm: %w", err)
	}
	return aead, nil
}
