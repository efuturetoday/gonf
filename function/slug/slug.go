// Package slug provides the gonf.Function slug, which makes its argument
// fit for a name: lower case letters, digits and '-', at most 63 long, as
// a tag of an image, a name in Kubernetes or a label of DNS takes it:
//
//	app.image.tag=${slug::${git.branch}}
//
// feature/Login #12 becomes feature-login-12. An argument without a letter
// or digit is an error, as its slug would be empty.
package slug

import (
	"fmt"
	"strings"

	"github.com/efuturetoday/gonf"
)

// Name is the name of Function in ${slug::text}.
const Name = "slug"

// maxLen is the longest slug: a label of DNS or a name in Kubernetes.
const maxLen = 63

// Function returns the gonf.Function slug.
func Function() gonf.Function {
	return gonf.Function{Name: Name, Call: func(_ *gonf.Config, s string, _ gonf.Origin) (string, error) {
		out := Make(s)
		if out == "" {
			return "", fmt.Errorf("%q has no letter or digit for a slug", s)
		}
		return out, nil
	}}
}

// Make returns the slug of s: its letters and digits of ASCII in lower
// case, every other run of characters as one '-', none at the ends, cut
// to 63.
func Make(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	out := b.String()
	if len(out) > maxLen {
		out = strings.TrimRight(out[:maxLen], "-")
	}
	return out
}
