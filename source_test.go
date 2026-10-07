package gonf_test

import (
	"testing"

	"github.com/efuturetoday/gonf"
)

func TestScheme(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		input          string
		expectedScheme string
		expectedOK     bool
	}{
		{name: "file path", input: "file:/etc/app.properties", expectedScheme: "file", expectedOK: true},
		{name: "empty location", input: "env:", expectedScheme: "env", expectedOK: true},
		{name: "k8s", input: "k8s:configmap/ns/app", expectedScheme: "k8s", expectedOK: true},
		{name: "allowed punctuation", input: "my-src+v1.2:x", expectedScheme: "my-src+v1.2", expectedOK: true},
		{name: "no colon", input: "defaults"},
		{name: "empty", input: ""},
		{name: "empty scheme", input: ":x"},
		{name: "windows drive letter", input: `C:\app.properties`},
		{name: "starts with digit", input: "1x:y"},
		{name: "invalid character", input: "my_src:x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scheme, ok := gonf.Scheme(tt.input)
			if scheme != tt.expectedScheme || ok != tt.expectedOK {
				t.Errorf("Scheme(%q) = %q, %v; want %q, %v", tt.input, scheme, ok, tt.expectedScheme, tt.expectedOK)
			}
		})
	}
}
