package metrics

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

type StorageCollector struct {
	storage     *pebble.Storage
	storageDir  string
	storageSize prometheus.Gauge
	//TODO: think of adding error metric indicating we couldn't update the db size
	interval time.Duration
	logger   zerolog.Logger
}

func NewStorageCollector(logger zerolog.Logger, storage *pebble.Storage, storageDir string, interval time.Duration) (*StorageCollector, error) {
	storageSize := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "storage_size_bytes",
			Help: "Estimated disk usage of storage in bytes",
		})

	if err := prometheus.Register(storageSize); err != nil {
		logger.Err(err).Msg("failed to register metric")
		return nil, err
	}

	return &StorageCollector{
		storage:     storage,
		storageDir:  storageDir,
		storageSize: storageSize,
		interval:    interval,
		logger:      logger,
	}, nil
}

func (c *StorageCollector) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.logger.Info().Msg("shutting down storage collector")
				return
			case <-ticker.C:
				c.updateStorageSize()
			}
		}
	}()
}

func (c *StorageCollector) updateStorageSize() {
	size, err := getFolderSize(c.storageDir)
	if err != nil {
		c.logger.Err(err).Msg("failed to get storage size. storage size metric will not be updated")
	} else {
		c.storageSize.Set(float64(size))
	}
}

func getFolderSize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}

		return nil
	})
	return size, err
}
