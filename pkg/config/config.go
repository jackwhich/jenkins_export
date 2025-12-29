package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"
)

// Server defines the general server configuration.
type Server struct {
	Addr    string
	Path    string
	Timeout time.Duration
	Web     string
	Pprof   bool
}

// Logs defines the level and color for log configuration.
type Logs struct {
	Level  string
	Pretty bool
}

// Target defines the target specific configuration.
type Target struct {
	Address  string
	Username string
	Password string
	Timeout  time.Duration
}

// Collector defines the collector specific configuration.
type Collector struct {
	Jobs            bool
	FetchBuildDetails bool // 是否获取构建详情（包括参数），默认true
	CacheFile      string // 缓存文件路径，如果为空则不使用缓存
	CacheTTL       time.Duration // 缓存过期时间，默认30分钟
	CacheRefreshInterval time.Duration // 定时刷新缓存的间隔，如果为0则不启用定时刷新
	FoldersStr     string // 要获取的文件夹列表（逗号分隔），如果为空则获取所有文件夹
	
	// SQLite 相关配置
	SQLitePath     string // SQLite 数据库路径，如果为空则不使用 SQLite
	DiscoveryInterval time.Duration // Job Discovery 同步间隔，默认5分钟
	CollectorInterval time.Duration // Build Collector 采集间隔，默认15秒（已废弃，不再使用定时采集）
	CollectorConcurrency int // Build Collector 并发数，默认10
}

// Config is a combination of all available configurations.
type Config struct {
	Server    Server
	Logs      Logs
	Target    Target
	Collector Collector
}

// Load initializes a default configuration struct.
func Load() *Config {
	return &Config{}
}

// Value returns the config value based on a DSN.
func Value(val string) (string, error) {
	if strings.HasPrefix(val, "file://") {
		content, err := os.ReadFile(
			strings.TrimPrefix(val, "file://"),
		)

		if err != nil {
			return "", fmt.Errorf("failed to parse secret file: %w", err)
		}

		return string(content), nil
	}

	if strings.HasPrefix(val, "base64://") {
		content, err := base64.StdEncoding.DecodeString(
			strings.TrimPrefix(val, "base64://"),
		)

		if err != nil {
			return "", fmt.Errorf("failed to parse base64 value: %w", err)
		}

		return string(content), nil
	}

	return val, nil
}
