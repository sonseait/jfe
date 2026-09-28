package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCommandsDoNotRequireDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid-database-url")
	var output bytes.Buffer
	if err := run([]string{"openapi"}, &output); err != nil {
		t.Fatal(err)
	}
	var spec struct {
		OpenAPI string `json:"openapi"`
	}
	if err := json.Unmarshal(output.Bytes(), &spec); err != nil || spec.OpenAPI != "3.1.0" {
		t.Fatalf("invalid OpenAPI output: %v", err)
	}
	for _, flag := range []string{"help", "-h", "--help"} {
		output.Reset()
		if err := run([]string{flag}, &output); err != nil || !strings.Contains(output.String(), "migrate CLI") {
			t.Fatalf("help %s: %v", flag, err)
		}
	}
	for _, args := range [][]string{{"migrate"}, {"unknown"}, {"openapi", "extra"}} {
		if err := run(args, io.Discard); err == nil {
			t.Fatalf("unexpectedly accepted args %v", args)
		}
	}
}
