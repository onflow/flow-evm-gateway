package metrics

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/models"
)

type StorageSizeCollector struct {
	//TODO: think of adding error metric indicating we couldn't update the db size
	storageDir  string
	storageSize prometheus.Gauge
	interval    time.Duration
	logger      zerolog.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	ready       chan struct{}
}

type StorageSizeCollectorOpts struct {
	Logger     zerolog.Logger
	StorageDir string
	Interval   time.Duration
}

func NewRestartableStorageSizeCollector(opts StorageSizeCollectorOpts, retries uint) models.Engine {
	engine := NewStorageSizeCollector(opts)
	return models.NewRestartableEngine(engine, retries, opts.Logger)
}

func NewStorageSizeCollector(opts StorageSizeCollectorOpts) *StorageSizeCollector {
	storageSize := promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "storage_size_bytes",
			Help: "Estimated disk usage of storage in bytes",
		})

	return &StorageSizeCollector{
		storageDir:  opts.StorageDir,
		storageSize: storageSize,
		interval:    opts.Interval,
		logger:      opts.Logger,
		ready:       make(chan struct{}),
	}
}

func (c *StorageSizeCollector) Run(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				c.logger.Warn().Msg("recovered from panic in storage size collector")
			}
		}()

		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		c.ready <- struct{}{}
		c.logger.Info().Msg("starting storage size collector")

		for {
			select {
			case <-c.ctx.Done():
				c.logger.Info().Msg("shutting down storage size collector")
				return
			case <-ticker.C:
				c.updateStorageSize()
			}
		}
	}()

	return nil
}

func (c *StorageSizeCollector) updateStorageSize() {
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

func (c *StorageSizeCollector) Stop() {
	c.cancel()
}

func (c *StorageSizeCollector) Ready() <-chan struct{} {
	return c.ready
}

func (c *StorageSizeCollector) Done() <-chan struct{} {
	return c.ctx.Done()
}
