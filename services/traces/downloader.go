package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
	"google.golang.org/api/option"
)

const downloadTimeout = 60 * time.Minute

type Downloader interface {
	// Download traces or returning an error with the failure
	Download(txID common.Hash, blockIO flow.Identifier) (json.RawMessage, error)
}

var _ Downloader = &GCPDownloader{}

type GCPDownloader struct {
	client *storage.Client
	logger zerolog.Logger
	bucket *storage.BucketHandle
}

func NewGCPDownloader(bucketName string, logger zerolog.Logger) (*GCPDownloader, error) {
	if bucketName == "" {
		return nil, fmt.Errorf("must provide bucket name")
	}

	ctx := context.Background()
	// we don't require authentication for public bucket
	client, err := storage.NewClient(ctx, option.WithoutAuthentication())
	if err != nil {
		return nil, fmt.Errorf("failed to create Google Cloud Storage client: %w", err)
	}

	return &GCPDownloader{
		client: client,
		logger: logger,
		bucket: client.Bucket(bucketName),
	}, nil
}

func (g *GCPDownloader) Download(txID common.Hash, blockID flow.Identifier) (json.RawMessage, error) {
	l := g.logger.With().
		Str("tx-id", txID.String()).
		Str("cadence-block-id", blockID.String()).
		Logger()

	l.Debug().Msg("downloading transaction trace")

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	id := fmt.Sprintf("%s-%s", blockID.String(), txID.String())

	rc, err := g.bucket.Object(id).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to download object id %s: %w", id, err)
	}
	defer rc.Close()

	trace, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read trace id %s: %w", id, err)
	}

	l.Info().Int("trace-size", len(trace)).Msg("transaction trace downloaded")

	return trace, nil
}
