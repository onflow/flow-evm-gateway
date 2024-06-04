package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
)

const downloadTimeout = 60 * time.Second

type Downloader interface {
	// Download traces or returning an error with the failure
	Download(id common.Hash) (json.RawMessage, error)
}

var _ Downloader = &GCPDownloader{}

type GCPDownloader struct {
	client     *storage.Client
	logger     zerolog.Logger
	bucketName string
}

func NewGCPDownloader(bucketName string, logger zerolog.Logger) (*GCPDownloader, error) {
	if bucketName == "" {
		return nil, fmt.Errorf("must provide bucket name")
	}

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage.NewClient: %w", err)
	}

	// try accessing buckets to validate settings
	_, err = client.Bucket(bucketName).Attrs(ctx)
	if err != nil {
		return nil, fmt.Errorf("error accessing bucket: %s, make sure bucket exists: %w", bucketName, err)
	}

	return &GCPDownloader{
		client:     client,
		logger:     logger,
		bucketName: bucketName,
	}, nil
}

func (g *GCPDownloader) Download(id common.Hash) (json.RawMessage, error) {
	l := g.logger.With().Str("tx-id", id.String()).Logger()
	l.Debug().Msg("downloading transaction trace")

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	rc, err := g.client.Bucket(g.bucketName).Object(id.String()).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to download id %s: %w", id, err)
	}
	defer rc.Close()

	trace, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read trace id %s: %w", id, err)
	}

	l.Info().Int("trace-size", len(trace)).Msg("transaction trace downloaded")

	return trace, nil
}
