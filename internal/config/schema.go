package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

type obj = map[string]any

// Schema returns the deterministic JSON Schema of drover.yaml, derived from the Config struct tags.
// Types with a custom YAML shape (ready, port, layout, restart) use a fixed shape instead.
func Schema() ([]byte, error) {
	root := structSchema(reflect.TypeFor[Config]())
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["title"] = "drover.yaml"
	root["required"] = []string{keyVersion, "name"}
	root["$defs"] = obj{"split": splitSchema()}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}

func structSchema(t reflect.Type) obj {
	props := obj{}
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		s := typeSchema(t.Name()+"."+f.Name, f.Type)
		if d := f.Tag.Get("desc"); d != "" {
			s["description"] = d
		}
		props[name] = s
	}

	return obj{jsType: jsObject, jsProperties: props, jsAdditional: false}
}

// typeSchema maps a Go type to a schema; field is "Struct.Field" and wins over the type for overrides.
func typeSchema(field string, t reflect.Type) obj {
	switch field {
	case "Process.Restart":
		return restartSchema()
	case "Config.Version":
		return obj{jsType: jsInteger, "const": SupportedVersion}
	}
	switch t.Name() {
	case "Ready":
		return readySchema()
	case "Port":
		return portSchema()
	case "Layout":
		return obj{jsOneOf: []obj{{jsEnum: layoutNames()}, {"$ref": "#/$defs/split"}}}
	}
	switch t.Kind() { //nolint:exhaustive // other kinds are not used in Config
	case reflect.String:
		return obj{jsType: jsString}
	case reflect.Int:
		return obj{jsType: jsInteger}
	case reflect.Slice:
		return obj{jsType: "array", "items": typeSchema("", t.Elem())}
	case reflect.Map:
		return obj{jsType: jsObject, jsAdditional: typeSchema("", t.Elem())}
	case reflect.Struct:
		return structSchema(t)
	}

	return obj{}
}

func readySchema() obj {
	str := obj{jsType: jsString}
	dur := obj{jsType: jsString, "description": "Go duration, e.g. 500ms, 5s, 2m."}

	return obj{
		jsType: jsObject,
		jsProperties: obj{
			"http": str, "tcp": str, "log": str, kindDelay: dur, keyTimeout: dur, kindNone: obj{"const": true},
		},
		jsAdditional:  false,
		"description": "At most one of http, tcp, log, delay or none; defaults to none.",
	}
}

func portSchema() obj {
	return obj{jsOneOf: []obj{
		{jsType: jsInteger, "minimum": 1, "maximum": maxPort},
		{
			jsType:       jsObject,
			jsProperties: obj{"from_env_file": obj{jsType: jsString}, "key": obj{jsType: jsString}},
			"required":   []string{"from_env_file", "key"},
			jsAdditional: false,
		},
	}}
}

func restartSchema() obj {
	return obj{jsOneOf: []obj{
		{jsEnum: []string{"never", policyOnFailure}},
		{
			jsType: jsObject,
			jsProperties: obj{policyOnFailure: obj{
				jsType:       jsObject,
				jsProperties: obj{"max": obj{jsType: jsInteger, "minimum": 0}},
				jsAdditional: false,
			}},
			"required":   []string{policyOnFailure},
			jsAdditional: false,
		},
	}}
}

func splitSchema() obj {
	return obj{
		jsType: jsObject,
		jsProperties: obj{
			"split":    obj{jsEnum: []string{dirColumns, dirRows}},
			"children": obj{jsType: "array", "minItems": 1, "items": obj{"$ref": "#/$defs/split"}},
			"group":    obj{jsType: jsString},
		},
		jsAdditional: false,
	}
}
