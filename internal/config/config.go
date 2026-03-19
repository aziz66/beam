package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port              int
	Host              string
	MaxRoomSize       int
	DefaultTTL        time.Duration
	MaxFileBuffer     int64 // bytes
	EnablePinnedRooms bool
	DataDir           string
	TLSCert           string
	TLSKey            string
	GracePeriod       time.Duration
	MaxRooms          int
}

func Load() *Config {
	c := &Config{}
	flag.IntVar(&c.Port, "port", envInt("BEAM_PORT", 8080), "Server port")
	flag.StringVar(&c.Host, "host", envStr("BEAM_HOST", "0.0.0.0"), "Bind address")
	flag.IntVar(&c.MaxRoomSize, "max-room-size", envInt("BEAM_MAX_ROOM_SIZE", 10), "Max devices per room")
	flag.DurationVar(&c.DefaultTTL, "default-ttl", envDuration("BEAM_DEFAULT_TTL", 30*time.Minute), "Default item TTL")
	flag.Int64Var(&c.MaxFileBuffer, "max-file-buffer", envInt64("BEAM_MAX_FILE_BUFFER", 256*1024*1024), "Max file buffer in bytes")
	flag.BoolVar(&c.EnablePinnedRooms, "enable-pinned-rooms", envBool("BEAM_ENABLE_PINNED_ROOMS", true), "Allow pinned rooms")
	flag.StringVar(&c.DataDir, "data-dir", envStr("BEAM_DATA_DIR", "./data"), "Data directory for pinned rooms")
	flag.StringVar(&c.TLSCert, "tls-cert", envStr("BEAM_TLS_CERT", ""), "TLS certificate path")
	flag.StringVar(&c.TLSKey, "tls-key", envStr("BEAM_TLS_KEY", ""), "TLS key path")
	flag.DurationVar(&c.GracePeriod, "grace-period", envDuration("BEAM_GRACE_PERIOD", 5*time.Minute), "Room grace period after last disconnect")
	flag.IntVar(&c.MaxRooms, "max-rooms", envInt("BEAM_MAX_ROOMS", 1000), "Maximum concurrent rooms")
	flag.Parse()
	return c
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
