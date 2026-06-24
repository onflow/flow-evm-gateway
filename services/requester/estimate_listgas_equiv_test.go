package requester

import (
	"math"
	"math/big"
	"testing"

	gethCore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	gethParams "github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// oldListGas reproduces the previous hand-coded access-list + authorization-list
// arithmetic that the refactor replaced, so we can assert exact equivalence.
func oldListGas(t *testing.T, accessList *types.AccessList, authList []types.SetCodeAuthorization, isAmsterdam bool) uint64 {
	var passingGasLimit uint64 // start from zero so the result is just the delta
	if accessList != nil {
		addresses := uint64(len(*accessList))
		storageKeys := uint64(accessList.StorageKeys())
		require.GreaterOrEqual(t, (math.MaxUint64-passingGasLimit)/gethParams.TxAccessListAddressGas, addresses)
		passingGasLimit += addresses * gethParams.TxAccessListAddressGas
		require.GreaterOrEqual(t, (math.MaxUint64-passingGasLimit)/gethParams.TxAccessListStorageKeyGas, storageKeys)
		passingGasLimit += storageKeys * gethParams.TxAccessListStorageKeyGas

		if isAmsterdam {
			const (
				addressCost    = common.AddressLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte
				storageKeyCost = common.HashLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte
			)
			passingGasLimit += addresses * addressCost
			passingGasLimit += storageKeys * storageKeyCost
		}
	}
	if authList != nil {
		if isAmsterdam {
			passingGasLimit += uint64(len(authList)) * gethParams.TxAuthTupleRegularGas
			passingGasLimit += uint64(len(authList)) * (gethParams.AuthorizationCreationSize + gethParams.AccountCreationSize) * gethParams.CostPerStateByte
		} else {
			passingGasLimit += uint64(len(authList)) * gethParams.CallNewAccountGas
		}
	}
	return passingGasLimit
}

// newListGas reproduces the refactored delta computation.
func newListGas(accessList *types.AccessList, authList []types.SetCodeAuthorization, rules gethParams.Rules) (uint64, error) {
	var al types.AccessList
	if accessList != nil {
		al = *accessList
	}
	withLists, err := gethCore.IntrinsicGas(nil, al, authList, false, rules, gethParams.CostPerStateByte)
	if err != nil {
		return 0, err
	}
	baseline, err := gethCore.IntrinsicGas(nil, nil, nil, false, rules, gethParams.CostPerStateByte)
	if err != nil {
		return 0, err
	}
	return withLists.Sum() - baseline.Sum(), nil
}

func TestListGasEquivalence(t *testing.T) {
	mkAccessList := func(addrs, keysPerAddr int) *types.AccessList {
		al := types.AccessList{}
		for i := 0; i < addrs; i++ {
			tup := types.AccessTuple{Address: common.BigToAddress(big.NewInt(int64(i + 1)))}
			for k := 0; k < keysPerAddr; k++ {
				tup.StorageKeys = append(tup.StorageKeys, common.BigToHash(big.NewInt(int64(k+1))))
			}
			al = append(al, tup)
		}
		return &al
	}
	mkAuthList := func(n int) []types.SetCodeAuthorization {
		if n == 0 {
			return nil
		}
		out := make([]types.SetCodeAuthorization, n)
		return out
	}

	accessCases := []*types.AccessList{
		nil,
		mkAccessList(1, 0),
		mkAccessList(3, 2),
		mkAccessList(5, 10),
	}
	authCases := [][]types.SetCodeAuthorization{
		nil,
		mkAuthList(1),
		mkAuthList(4),
	}

	// Build a chain config that activates everything up to Amsterdam, then toggle
	// the fork boundary purely via timestamp.
	cfg := *gethParams.MainnetChainConfig
	amsterdamTime := uint64(1000)
	cfg.OsakaTime = &amsterdamTime
	cfg.AmsterdamTime = &amsterdamTime

	for _, isAmsterdam := range []bool{false, true} {
		blockTime := uint64(0)
		if isAmsterdam {
			blockTime = amsterdamTime + 1
		}
		// Use a post-London block number, since Rules gates all timestamp-based
		// forks behind `isMerge && IsLondon(num)`.
		rules := cfg.Rules(big.NewInt(20_000_000), true, blockTime)
		require.Equal(t, isAmsterdam, rules.IsAmsterdam)

		for _, al := range accessCases {
			for _, auth := range authCases {
				if al == nil && auth == nil {
					continue
				}
				want := oldListGas(t, al, auth, isAmsterdam)
				got, err := newListGas(al, auth, rules)
				require.NoError(t, err)
				require.Equalf(t, want, got,
					"mismatch isAmsterdam=%v accessList=%v authList=%d", isAmsterdam, al, len(auth))
			}
		}
	}
}
