package logging

import (
	"context"
	"log"
	"time"
)

// RetentionConfig controls the log retention background loop.
type RetentionConfig struct {
	CronLogDir   string        // path to logs/cron/jobs/
	DaemonLogDir string        // path to logs/
	MaxAgeDays   int           // default 7
	MaxFiles     int           // default 1000
	Compress     bool          // default false
	Interval     time.Duration // default 6 hours
}

// StartRetentionLoop runs EnforceRetention on the configured directories at
// the specified interval. It runs immediately on the first call (so old files
// are cleaned up on daemon startup) and then repeats on the configured
// interval. Call from daemon startup; it blocks until ctx is cancelled.
func StartRetentionLoop(ctx context.Context, cfg RetentionConfig) {
	// Apply defaults
	if cfg.MaxAgeDays <= 0 {
		cfg.MaxAgeDays = 7
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = 1000
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}

	// Run immediately on startup
	runRetention(cfg)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runRetention(cfg)
		}
	}
}

func runRetention(cfg RetentionConfig) {
	dirs := []string{cfg.DaemonLogDir, cfg.CronLogDir}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := EnforceRetention(dir, cfg.MaxAgeDays, cfg.MaxFiles, cfg.Compress); err != nil {
			log.Printf("log retention: error processing %s: %v", dir, err)
		}
	}
}
