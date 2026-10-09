package boot

import (
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
)

func TestSchema(t *testing.T) {
	t.Parallel()
	type cfg struct {
		URL string `desc:"Address."`
	}
	doc, err := Schema(WithMappings(gonf.NewMapping[cfg]("db")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"url"`) || !strings.Contains(string(doc), "Address.") {
		t.Errorf("schema lacks the key: %s", doc)
	}
	if _, err := Schema(WithMappings(nil)); err == nil {
		t.Error("an invalid mapping must be an error")
	}
}
