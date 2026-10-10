// Package uuid provides the gonf.Function uuid, which returns a random UUID
// (version 4):
//
//	app.instance-id=${uuid::}
//
// gonf caches results per Load by argument, so ${uuid::} is the same
// everywhere in one Config, and ${uuid::a} and ${uuid::b} differ. Every
// Load, and every reload of Watch, gives new UUIDs; keep a UUID that must
// survive reloads elsewhere.
package uuid

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/efuturetoday/gonf"
)

// Name is the name of Function in ${uuid::}.
const Name = "uuid"

// Function returns the gonf.Function uuid. Its argument only names the
// UUID for the cache.
func Function() gonf.Function {
	return gonf.Function{Name: Name, Call: func(*gonf.Config, string, gonf.Origin) (string, error) {
		return random(), nil
	}}
}

// random returns a random UUID of version 4, such as
// "0b5f7e2c-3d1a-4c6e-9f8b-2a7d4e6c1b3f", from crypto/rand.
func random() string {
	var u [16]byte
	_, _ = rand.Read(u[:])  // never fails, see crypto/rand.Read
	u[6] = u[6]&0x0f | 0x40 // version 4
	u[8] = u[8]&0x3f | 0x80 // variant RFC 9562
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:], u[10:])
	return string(b[:])
}
