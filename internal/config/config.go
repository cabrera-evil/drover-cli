package config

import "os"

type Config struct {
	Name string
}

const defaultName = "World"

func Load() Config {
	name := os.Getenv("APP_NAME")
	if name == "" {
		name = defaultName
	}

	return Config{
		Name: name,
	}
}
