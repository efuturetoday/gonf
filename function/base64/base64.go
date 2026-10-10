// Package base64 provides the gonf.Functions base64 and base64-decode,
// with the standard encoding of RFC 4648, padded:
//
//	tls.cert=${base64::${file::/run/secrets/tls.crt}}
//	api.key=${base64-decode::czNjcmV0}
//
// Their results are no secrets, unless the argument is one, as the content
// of a file is.
package base64

import (
	"encoding/base64"
	"errors"

	"github.com/efuturetoday/gonf"
)

// Names of the Functions in ${base64::text} and ${base64-decode::text}.
const (
	EncodeName = "base64"
	DecodeName = "base64-decode"
)

// Encode returns the gonf.Function base64, which encodes its argument.
func Encode() gonf.Function {
	return gonf.Function{Name: EncodeName, Call: encode}
}

// Decode returns the gonf.Function base64-decode, which decodes its
// argument. Invalid input is an error that does not show it.
func Decode() gonf.Function {
	return gonf.Function{Name: DecodeName, Call: decode}
}

func encode(_ *gonf.Config, s string, _ gonf.Origin) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}

var errInvalid = errors.New("argument is not standard base64")

func decode(_ *gonf.Config, s string, _ gonf.Origin) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", errInvalid
	}
	return string(b), nil
}
