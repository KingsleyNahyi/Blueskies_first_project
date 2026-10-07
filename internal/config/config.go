package config

import (
	"os"
)

// Config holds everything the app needs to boot. All values come from
// env vars so the same binary runs locally (sqlite file) and on
// Turso in prod, without a rebuild.
type Config struct {
	Port         string
	DatabaseURL  string // e.g. "file:local.db" for dev, or "libsql://<db>.turso.io"
	DatabaseAuth string // Turso auth token; empty for local sqlite
	JWTSecret    string
	Env          string // "dev" | "prod"
}

//loading the environment functinos using the get env helpers. 
func Load() Config {
	return Config{
		Port:         getEnv("PORT", "8080"),
		DatabaseURL:  getEnv("DATABASE_URL", "file:local.db"),
		DatabaseAuth: getEnv("DATABASE_AUTH_TOKEN", ""),
		JWTSecret:    getEnv("JWT_SECRET", "dev-secret-change-me"),
		Env:          getEnv("APP_ENV", "dev"),
	}
}

//this getenv function  is a helper essentially gets the environment values through the key or uses a fallback/default string 

func getEnv(key, fallback string) string {
	//attempt to optain the key
	v := os.Getenv(key)

	
	if v != "" {
		return v
	}

	//if we dont get a key we return fallback . 
	return fallback
}
