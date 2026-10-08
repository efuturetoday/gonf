module github.com/efuturetoday/gonf/format/yaml

go 1.23

require (
	github.com/efuturetoday/gonf v0.0.0
	go.yaml.in/yaml/v3 v3.0.5
)

// Until the first release, the module is built against the gonf in this
// repository.
replace github.com/efuturetoday/gonf => ../..
