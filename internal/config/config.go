package config

import (
	"errors"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
	"io/fs"
	"log"
	"os"
	"time"
)

const (
	envPath          = ".env"
	configEnvPathVar = "CONFIG_PATH"
)

type Config struct {
	Env         string     `yaml:"env" env-default:"local"`
	StoragePath string     `yaml:"storage_path" env_required:"true"`
	HTTPServer  HTTPServer `yaml:"http_server"`
}

type HTTPServer struct {
	Address     string        `yaml:"address" env-default:"localhost:8080"`
	Timeout     time.Duration `yaml:"timeout" env-default:"4s"`
	IdleTimeout time.Duration `yaml:"idle_timeout" env-default:"60s"`
}

func MustLoad() *Config {
	err := godotenv.Load(envPath)
	if err != nil {
		log.Fatalf("Failed to load env on path '%s', error: %s", envPath, err)
	}

	configPath := os.Getenv(configEnvPathVar)
	if configPath == "" {
		log.Fatalf("Config variable %s was not set in .env", configEnvPathVar)
	}

	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("No config file exists on path scecified: %s", err)
	}

	var cfg Config

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("Error while reading config: %s", err)
	}

	return &cfg
}
