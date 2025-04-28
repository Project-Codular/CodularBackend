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
	Env           string        `yaml:"env" env-default:"local"`
	StoragePath   string        `yaml:"storage_path" env_required:"true"`
	DBCredentials DBCredentials `yaml:"db_credentials"`
	HTTPServer    HTTPServer    `yaml:"http_server"`
	AliasLength   int           `yaml:"alias_length"`
}

type HTTPServer struct {
	Address     string        `yaml:"address" env-default:"localhost:8080"`
	Timeout     time.Duration `yaml:"timeout" env-default:"4s"`
	IdleTimeout time.Duration `yaml:"idle_timeout" env-default:"60s"`
}

type DBCredentials struct {
	SuperUser         string `yaml:"super_user" env_required:"true"`
	SuperUserPassword string `yaml:"super_user_password" env_required:"true"`
	Name              string `yaml:"name" env_required:"true"`
	User              string `yaml:"user" env_required:"true"`
	Password          string `yaml:"password" env_required:"true"`
	Port              int    `yaml:"port" env-default:"5432"`
	HostName          string `yaml:"host_name" env-default:"localhost"`
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
