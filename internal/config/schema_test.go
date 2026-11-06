package config_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
)

var update = flag.Bool("update", false, "regenerate schema/drover.schema.json") //nolint:gochecknoglobals // test flag

const schemaPath = "../../schema/drover.schema.json"

// AC11 (R13): schema is generated from Go structs; regenerating yields no diff.
// Run `go test ./internal/config -run Schema -update` to (re)generate the checked-in file.
func TestSchemaGolden(t *testing.T) {
	got, err := config.Schema()
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if *update {
		if werr := os.WriteFile(schemaPath, got, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	want, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("checked-in schema missing: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("schema drift; regenerate with -update")
	}
}

func TestSchemaShape(t *testing.T) {
	raw, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if uerr := json.Unmarshal(raw, &doc); uerr != nil {
		t.Fatalf("invalid JSON: %v", uerr)
	}
	if _, ok := doc["$schema"]; !ok {
		t.Error("missing $schema")
	}
	props, _ := doc["properties"].(map[string]any)
	for _, k := range []string{"version", "name", "processes", "groups", "presets", "layout"} {
		if _, ok := props[k]; !ok {
			t.Errorf("schema missing property %q", k)
		}
	}
}

func TestSchemaDeterministic(t *testing.T) {
	a, _ := config.Schema()
	b, _ := config.Schema()
	if !bytes.Equal(a, b) {
		t.Error("Schema() output is not deterministic")
	}
}
