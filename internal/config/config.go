package config

import (
	"flag"
	"log"
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
	TrustedProxy      bool // trust X-Forwarded-For for rate limiting (only set when behind a known proxy)
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
	flag.BoolVar(&c.TrustedProxy, "trusted-proxy", envBool("BEAM_TRUSTED_PROXY", false), "Trust X-Forwarded-For header (set only when behind a known reverse proxy)")
	flag.Parse()
	c.validate()
	return c
}

// validate logs fatal errors for out-of-range values and clamps others to safe
// defaults so the server never starts in a silently broken configuration.
func (c *Config) validate() {
	if c.Port < 1 || c.Port > 65535 {
		log.Fatalf("config: port %d is out of range (1-65535)", c.Port)
	}
	if c.MaxRoomSize <= 0 {
		log.Printf("config: max-room-size %d is invalid, resetting to 10", c.MaxRoomSize)
		c.MaxRoomSize = 10
	}
	if c.DefaultTTL <= 0 {
		log.Printf("config: default-ttl %v is invalid, resetting to 30m", c.DefaultTTL)
		c.DefaultTTL = 30 * time.Minute
	}
	if c.GracePeriod < 0 {
		log.Printf("config: grace-period %v is invalid, resetting to 5m", c.GracePeriod)
		c.GracePeriod = 5 * time.Minute
	}
	if c.MaxRooms <= 0 {
		log.Printf("config: max-rooms %d is invalid, resetting to 1000", c.MaxRooms)
		c.MaxRooms = 1000
	}
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
		log.Printf("config: invalid %s=%q, using default %d", key, v, fallback)
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
		log.Printf("config: invalid %s=%q, using default %d", key, v, fallback)
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		log.Printf("config: invalid %s=%q, using default %v", key, v, fallback)
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("config: invalid %s=%q, using default %v", key, v, fallback)
	}
	return fallback
}
