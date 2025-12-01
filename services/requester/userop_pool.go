package requester

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
)

const userOpPoolSize = 10_000

// UserOperationPool manages the alt-mempool for UserOperations
type UserOperationPool interface {
	Add(ctx context.Context, userOp *models.UserOperation, entryPoint common.Address) (common.Hash, error)
	GetByHash(hash common.Hash) (*models.UserOperation, error)
	GetPending() []*models.UserOperation
	Remove(hash common.Hash)
	GetBySender(sender common.Address) []*models.UserOperation
}

// InMemoryUserOpPool is an in-memory implementation of UserOperationPool
type InMemoryUserOpPool struct {
	// userOps stores UserOperations by their hash
	userOps sync.Map // map[common.Hash]*models.UserOperation
	// userOpsBySender groups UserOperations by sender address for nonce ordering
	userOpsBySender sync.Map // map[common.Address][]*pooledUserOp
	// userOpsByHash maps hash to sender+nonce for quick lookup
	userOpsByHash sync.Map // map[common.Hash]*pooledUserOp
	// TTL tracking
	ttlCache *expirable.LRU[common.Hash, time.Time]
	config   config.Config
	logger   zerolog.Logger
	mux      sync.RWMutex
}

type pooledUserOp struct {
	userOp     *models.UserOperation
	hash       common.Hash
	sender     common.Address
	nonce      *big.Int
	addedAt    time.Time
	entryPoint common.Address
}

var _ UserOperationPool = &InMemoryUserOpPool{}

func NewInMemoryUserOpPool(config config.Config, logger zerolog.Logger) *InMemoryUserOpPool {
	ttlCache := expirable.NewLRU[common.Hash, time.Time](
		userOpPoolSize,
		nil,
		config.UserOpTTL,
	)

	return &InMemoryUserOpPool{
		ttlCache: ttlCache,
		config:   config,
		logger:   logger.With().Str("component", "userop-pool").Logger(),
	}
}

func (p *InMemoryUserOpPool) Add(
	ctx context.Context,
	userOp *models.UserOperation,
	entryPoint common.Address,
) (common.Hash, error) {
	// Calculate UserOperation hash
	chainID := p.config.EVMNetworkID
	hash, err := userOp.Hash(entryPoint, chainID)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to hash user operation: %w", err)
	}

	p.mux.Lock()
	defer p.mux.Unlock()

	// Check for duplicates
	if _, exists := p.userOpsByHash.Load(hash); exists {
		return hash, fmt.Errorf("duplicate user operation: %s", hash.Hex())
	}

	// Check for nonce conflicts with existing UserOps from same sender
	if existingOps, ok := p.userOpsBySender.Load(userOp.Sender); ok {
		ops := existingOps.([]*pooledUserOp)
		for _, op := range ops {
			if op.nonce.Cmp(userOp.Nonce) == 0 {
				return common.Hash{}, fmt.Errorf("nonce conflict: sender %s already has UserOp with nonce %s", userOp.Sender.Hex(), userOp.Nonce.String())
			}
		}
	}

	// Create pooled entry
	pooled := &pooledUserOp{
		userOp:     userOp,
		hash:       hash,
		sender:     userOp.Sender,
		nonce:      userOp.Nonce,
		addedAt:    time.Now(),
		entryPoint: entryPoint,
	}

	// Store by hash
	p.userOps.Store(hash, userOp)
	p.userOpsByHash.Store(hash, pooled)
	p.ttlCache.Add(hash, time.Now())

	// Store by sender for nonce ordering
	var ops []*pooledUserOp
	if existing, ok := p.userOpsBySender.Load(userOp.Sender); ok {
		ops = existing.([]*pooledUserOp)
	}
	ops = append(ops, pooled)
	p.userOpsBySender.Store(userOp.Sender, ops)

	p.logger.Debug().
		Str("hash", hash.Hex()).
		Str("sender", userOp.Sender.Hex()).
		Str("nonce", userOp.Nonce.String()).
		Msg("user operation added to pool")

	return hash, nil
}

func (p *InMemoryUserOpPool) GetByHash(hash common.Hash) (*models.UserOperation, error) {
	if userOp, ok := p.userOps.Load(hash); ok {
		// Check if entry exists in TTL cache (expirable LRU automatically handles expiration)
		// Get returns (value, ok) where ok indicates if the key exists and is not expired
		_, exists := p.ttlCache.Get(hash)
		if exists {
			return userOp.(*models.UserOperation), nil
		}
		// Entry expired or doesn't exist in cache - remove it
		p.Remove(hash)
	}
	return nil, fmt.Errorf("user operation not found: %s", hash.Hex())
}

func (p *InMemoryUserOpPool) GetPending() []*models.UserOperation {
	var pending []*models.UserOperation

	p.userOps.Range(func(key, value interface{}) bool {
		hash := key.(common.Hash)
		// Check if entry exists in TTL cache (not expired)
		if _, exists := p.ttlCache.Get(hash); exists {
			pending = append(pending, value.(*models.UserOperation))
		}
		return true
	})

	return pending
}

func (p *InMemoryUserOpPool) Remove(hash common.Hash) {
	p.mux.Lock()
	defer p.mux.Unlock()

	if pooled, ok := p.userOpsByHash.Load(hash); ok {
		po := pooled.(*pooledUserOp)

		// Remove from userOps
		p.userOps.Delete(hash)
		p.userOpsByHash.Delete(hash)
		p.ttlCache.Remove(hash)

		// Remove from sender list
		if existing, ok := p.userOpsBySender.Load(po.sender); ok {
			ops := existing.([]*pooledUserOp)
			var newOps []*pooledUserOp
			for _, op := range ops {
				if op.hash != hash {
					newOps = append(newOps, op)
				}
			}
			if len(newOps) == 0 {
				p.userOpsBySender.Delete(po.sender)
			} else {
				p.userOpsBySender.Store(po.sender, newOps)
			}
		}
	}
}

func (p *InMemoryUserOpPool) GetBySender(sender common.Address) []*models.UserOperation {
	if existing, ok := p.userOpsBySender.Load(sender); ok {
		ops := existing.([]*pooledUserOp)
		var userOps []*models.UserOperation
		for _, op := range ops {
			// Check if entry exists in TTL cache (not expired)
			if _, exists := p.ttlCache.Get(op.hash); exists {
				userOps = append(userOps, op.userOp)
			}
		}
		return userOps
	}
	return nil
}

