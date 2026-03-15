package state

import (
	"github.com/redis/go-redis/v9"
)

const (
	// LIDToPhoneKeyPrefix is the Redis key prefix for LID → phone number cache.
	LIDToPhoneKeyPrefix = "watgbridge:lid2pn:"
)

// NewRedisClient creates a Redis client from config. Caller must check cfg.Redis.Addr != "" first.
func NewRedisClient(cfg *Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
}
