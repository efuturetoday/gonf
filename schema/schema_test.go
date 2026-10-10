package schema_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/schema"
)

var update = flag.Bool("update", false, "rewrite the golden files")

type dbConfig struct {
	URL      string              `desc:"Address of the **database**."`
	Password gonf.Secret[string] `desc:"Password of the user."`
	Timeout  time.Duration       `default:"5s"`
	Debug    bool                `default:"true"`
	Ratio    float64             `default:"0.5"`
	Port     uint16              `default:"5432"`
	Tags     []string            `default:"a,b"`
	Pool     struct {
		MaxSize int `default:"10" desc:"Maximum number of open connections."`
	}
	Replica *struct {
		Host string
		Port int `default:"5432"`
	}
	Replicas []struct {
		Host string
	}
	Servers map[string]struct {
		Port *int
		Host string
	}
	Labels map[string]string
	Proxy  *url.URL
}

func describe(t *testing.T) []gonf.Property {
	t.Helper()
	props, err := gonf.Describe(
		gonf.WithMappings(gonf.NewMapping[dbConfig]("db")),
		gonf.WithConverter(url.Parse),
	)
	if err != nil {
		t.Fatal(err)
	}
	return props
}

func TestNew_Golden(t *testing.T) {
	t.Parallel()
	got, err := schema.New(describe(t))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("not valid JSON:\n%s", got)
	}
	const file = "testdata/db.json"
	if *update {
		if err := os.WriteFile(file, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("schema differs from %s (run go test ./schema -update):\n%s", file, got)
	}
}

func TestNew_Stable(t *testing.T) {
	t.Parallel()
	props := describe(t)
	first, err := schema.New(props)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		again, err := schema.New(props)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("two runs give different documents")
		}
	}
}

// decode returns the document as a tree and fails the test on invalid JSON.
func decode(t *testing.T, props []gonf.Property) map[string]any {
	t.Helper()
	b, err := schema.New(props)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// at follows the properties named by path.
func at(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, name := range path {
		props, ok := cur["properties"].(map[string]any)
		if !ok {
			t.Fatalf("no properties before %q in %v", name, cur)
		}
		cur, ok = props[name].(map[string]any)
		if !ok {
			t.Fatalf("no property %q in %v", name, props)
		}
	}
	return cur
}

func TestNew_Structure(t *testing.T) {
	t.Parallel()
	doc := decode(t, describe(t))

	if doc["$schema"] != schema.Dialect {
		t.Errorf("$schema = %v", doc["$schema"])
	}
	if _, ok := doc["additionalProperties"]; ok {
		t.Error("the root must stay open")
	}
	db := at(t, doc, "db")
	if db["additionalProperties"] != false {
		t.Errorf("db additionalProperties = %v, want false", db["additionalProperties"])
	}
	if got := db["required"]; len(got.([]any)) != 2 {
		t.Errorf("db required = %v, want url and password", got)
	}
	if got := at(t, doc, "db", "pool")["additionalProperties"]; got != false {
		t.Errorf("db.pool additionalProperties = %v, want false", got)
	}

	pw := at(t, doc, "db", "password")
	if pw["writeOnly"] != true {
		t.Error("secret is not writeOnly")
	}
	if _, ok := pw["default"]; ok {
		t.Error("secret has a default")
	}

	url := at(t, doc, "db", "url")
	if url["description"] != url["markdownDescription"] || url["description"] == "" {
		t.Errorf("description = %v, markdownDescription = %v", url["description"], url["markdownDescription"])
	}

	// Defaults are typed.
	if got := at(t, doc, "db", "port")["default"]; got != float64(5432) {
		t.Errorf("port default = %#v", got)
	}
	if got := at(t, doc, "db", "debug")["default"]; got != true {
		t.Errorf("debug default = %#v", got)
	}
	if got := at(t, doc, "db", "timeout")["default"]; got != "5s" {
		t.Errorf("timeout default = %#v", got)
	}

	// Map entries and list elements.
	labels := at(t, doc, "db", "labels")
	if labels["type"] != "object" || labels["additionalProperties"] == nil {
		t.Errorf("labels = %v", labels)
	}
	reps := at(t, doc, "db", "replicas")
	if reps["type"] != "array" || reps["items"] == nil {
		t.Errorf("replicas = %v", reps)
	}
	servers := at(t, doc, "db", "servers")["additionalProperties"].(map[string]any)
	if servers["type"] != "object" {
		t.Errorf("servers entry = %v", servers)
	}

	// Required applies inside an optional struct, not to the struct.
	replica := at(t, doc, "db", "replica")
	if got := replica["required"]; len(got.([]any)) != 1 {
		t.Errorf("replica required = %v, want host", got)
	}

	// Profiles.
	pat, ok := doc["patternProperties"].(map[string]any)
	if !ok || len(pat) != 2 {
		t.Fatalf("patternProperties = %v", doc["patternProperties"])
	}
	profile := pat[`^%[^.]+$`].(map[string]any)
	pdb := at(t, profile, "db")
	if _, ok := pdb["required"]; ok {
		t.Error("a profile section must not require keys")
	}
}

func TestNew_Types(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ  string
		want string
	}{
		{"int8", `"maximum":127`},
		{"uint32", `"maximum":4294967295`},
		{"uint", `"minimum":0`},
		{"[]int", `"array"`},
		{"*float64", `"number"`},
		{"main.Port", `"boolean"`},
	} {
		b, err := schema.New([]gonf.Property{{Key: "x.v", Type: tc.typ, Mapping: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, b); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(compact.Bytes(), []byte(tc.want)) {
			t.Errorf("%s: %s lacks %s", tc.typ, compact, tc.want)
		}
	}
}

func TestNew_Empty(t *testing.T) {
	t.Parallel()
	b, err := schema.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["type"] != "object" || doc["patternProperties"] != nil {
		t.Errorf("doc = %v", doc)
	}
}

func TestNew_Conflict(t *testing.T) {
	t.Parallel()
	_, err := schema.New([]gonf.Property{
		{Key: "a.b", Type: "string", Mapping: "a"},
		{Key: "a.b.c", Type: "string", Mapping: "a"},
	})
	if err == nil {
		t.Error("a single value that holds keys must be an error")
	}
	_, err = schema.New([]gonf.Property{
		{Key: "a.b.c", Type: "string", Mapping: "a"},
		{Key: "a.b", Type: "string", Mapping: "a"},
	})
	if err == nil {
		t.Error("an object that is a single value must be an error")
	}
}

func TestNew_AllowUnknownStaysOpen(t *testing.T) {
	t.Parallel()
	type cfg struct {
		Name string `default:"x"`
		Pool struct {
			Size int `default:"1"`
		}
	}
	props, err := gonf.Describe(gonf.WithMappings(
		gonf.NewMapping[cfg]("open", gonf.AllowUnknown()),
		gonf.NewMapping[cfg]("shut"),
	))
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, props)
	for _, path := range [][]string{{"open"}, {"open", "pool"}} {
		if _, ok := at(t, doc, path...)["additionalProperties"]; ok {
			t.Errorf("%v must stay open", path)
		}
	}
	for _, path := range [][]string{{"shut"}, {"shut", "pool"}} {
		if got := at(t, doc, path...)["additionalProperties"]; got != false {
			t.Errorf("%v additionalProperties = %v, want false", path, got)
		}
	}
}
