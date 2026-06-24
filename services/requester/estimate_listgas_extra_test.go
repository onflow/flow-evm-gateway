package requester

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethParams "github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// rulesForAmsterdam returns chain rules with Amsterdam active (timestamp >> fork
// timestamp) using a post-London block number so timestamp-gated forks are
// evaluated.
func rulesForAmsterdam(isAmsterdam bool) gethParams.Rules {
	cfg := *gethParams.MainnetChainConfig
	forkTime := uint64(1000)
	cfg.OsakaTime = &forkTime
	cfg.AmsterdamTime = &forkTime

	var blockTime uint64
	if isAmsterdam {
		blockTime = forkTime + 1
	}
	return cfg.Rules(big.NewInt(20_000_000), true, blockTime)
}

// TestNewListGas_BothNilReturnsZero verifies that when both the access list and
// the authorisation list are nil the delta is exactly zero. The main
// TestListGasEquivalence loop skips the (nil, nil) combination with continue, so
// this case would otherwise be untested.
func TestNewListGas_BothNilReturnsZero(t *testing.T) {
	for _, isAmsterdam := range []bool{false, true} {
		rules := rulesForAmsterdam(isAmsterdam)
		got, err := newListGas(nil, nil, rules)
		require.NoError(t, err)
		require.Zerof(t, got, "expected zero gas for nil lists (isAmsterdam=%v)", isAmsterdam)
	}
}

// TestNewListGas_AccessListOnly_AbsoluteValues checks concrete gas values for an
// access list when no authorisation list is present, both pre- and post-Amsterdam.
//
// Pre-Amsterdam per-address cost  = TxAccessListAddressGas (2400)
// Pre-Amsterdam per-key cost      = TxAccessListStorageKeyGas (1900)
//
// Amsterdam adds a floor surcharge per EIP-7981:
//   addressSurcharge = AddressLength(20) * TxCostFloorPerToken7976(16) * TxTokenPerNonZeroByte(4) = 1280
//   keySurcharge     = HashLength(32)    * TxCostFloorPerToken7976(16) * TxTokenPerNonZeroByte(4) = 2048
func TestNewListGas_AccessListOnly_AbsoluteValues(t *testing.T) {
	const (
		addrGas       = gethParams.TxAccessListAddressGas                                                                       // 2400
		keyGas        = gethParams.TxAccessListStorageKeyGas                                                                    // 1900
		addrSurcharge = common.AddressLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte            // 1280
		keySurcharge  = common.HashLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte               // 2048
	)

	cases := []struct {
		name          string
		addrs         int
		keysPerAddr   int
		wantPreAmst   uint64
		wantAmsterdam uint64
	}{
		{
			name:          "one address no keys",
			addrs:         1,
			keysPerAddr:   0,
			wantPreAmst:   1 * addrGas,
			wantAmsterdam: 1 * (addrGas + addrSurcharge),
		},
		{
			name:          "one address one key",
			addrs:         1,
			keysPerAddr:   1,
			wantPreAmst:   1*addrGas + 1*keyGas,
			wantAmsterdam: 1*(addrGas+addrSurcharge) + 1*(keyGas+keySurcharge),
		},
		{
			name:          "three addresses two keys each",
			addrs:         3,
			keysPerAddr:   2,
			wantPreAmst:   3*addrGas + 6*keyGas,
			wantAmsterdam: 3*(addrGas+addrSurcharge) + 6*(keyGas+keySurcharge),
		},
		{
			name:          "zero addresses (empty access list)",
			addrs:         0,
			keysPerAddr:   0,
			wantPreAmst:   0,
			wantAmsterdam: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			al := types.AccessList{}
			for i := 0; i < tc.addrs; i++ {
				tup := types.AccessTuple{
					Address: common.BigToAddress(big.NewInt(int64(i + 1))),
				}
				for k := 0; k < tc.keysPerAddr; k++ {
					tup.StorageKeys = append(tup.StorageKeys, common.BigToHash(big.NewInt(int64(k+1))))
				}
				al = append(al, tup)
			}
			alPtr := &al

			for _, isAmsterdam := range []bool{false, true} {
				rules := rulesForAmsterdam(isAmsterdam)
				want := tc.wantPreAmst
				if isAmsterdam {
					want = tc.wantAmsterdam
				}
				got, err := newListGas(alPtr, nil, rules)
				require.NoError(t, err)
				require.Equalf(t, want, got,
					"isAmsterdam=%v tc=%s", isAmsterdam, tc.name)
			}
		})
	}
}

// TestNewListGas_AuthListOnly_AbsoluteValues checks concrete gas values for an
// authorisation list when no access list is present.
//
// Pre-Amsterdam: CallNewAccountGas (25000) per tuple
// Amsterdam:     TxAuthTupleRegularGas(7500) + (AuthorizationCreationSize(23) + AccountCreationSize(120)) * CostPerStateByte(1530)
//              = 7500 + 143 * 1530 = 226290 per tuple
func TestNewListGas_AuthListOnly_AbsoluteValues(t *testing.T) {
	const (
		preAmstPerTuple  = gethParams.CallNewAccountGas // 25000
		amstPerTuple     = gethParams.TxAuthTupleRegularGas +
			(gethParams.AuthorizationCreationSize+gethParams.AccountCreationSize)*gethParams.CostPerStateByte // 226290
	)

	for _, n := range []int{1, 2, 5} {
		authList := make([]types.SetCodeAuthorization, n)

		for _, isAmsterdam := range []bool{false, true} {
			rules := rulesForAmsterdam(isAmsterdam)
			want := uint64(n) * preAmstPerTuple
			if isAmsterdam {
				want = uint64(n) * amstPerTuple
			}
			got, err := newListGas(nil, authList, rules)
			require.NoError(t, err)
			require.Equalf(t, want, got,
				"n=%d isAmsterdam=%v", n, isAmsterdam)
		}
	}
}

// TestNewListGas_AmsterdamAlwaysHigherOrEqual verifies that activating Amsterdam
// never reduces the intrinsic gas for a given set of lists, because EIP-7981 adds
// a floor surcharge on top of existing costs.
func TestNewListGas_AmsterdamAlwaysHigherOrEqual(t *testing.T) {
	makeAL := func(addrs, keys int) *types.AccessList {
		al := types.AccessList{}
		for i := 0; i < addrs; i++ {
			tup := types.AccessTuple{Address: common.BigToAddress(big.NewInt(int64(i + 1)))}
			for k := 0; k < keys; k++ {
				tup.StorageKeys = append(tup.StorageKeys, common.BigToHash(big.NewInt(int64(k+1))))
			}
			al = append(al, tup)
		}
		return &al
	}

	alCases := []*types.AccessList{nil, makeAL(1, 0), makeAL(2, 3)}
	authCases := [][]types.SetCodeAuthorization{nil, make([]types.SetCodeAuthorization, 1), make([]types.SetCodeAuthorization, 3)}

	rulesPreAmst := rulesForAmsterdam(false)
	rulesAmst := rulesForAmsterdam(true)

	for _, al := range alCases {
		for _, auth := range authCases {
			pre, err := newListGas(al, auth, rulesPreAmst)
			require.NoError(t, err)
			post, err := newListGas(al, auth, rulesAmst)
			require.NoError(t, err)
			require.GreaterOrEqualf(t, post, pre,
				"Amsterdam gas should be >= pre-Amsterdam gas (al=%v auth=%v)", al, len(auth))
		}
	}
}

// TestNewListGas_IsMonotonicallyIncreasing verifies that adding more entries to
// the access list or the authorisation list strictly increases the gas cost,
// holding the fork constant.
func TestNewListGas_IsMonotonicallyIncreasing(t *testing.T) {
	makeAL := func(addrs int) *types.AccessList {
		al := types.AccessList{}
		for i := 0; i < addrs; i++ {
			al = append(al, types.AccessTuple{
				Address: common.BigToAddress(big.NewInt(int64(i + 1))),
			})
		}
		return &al
	}

	for _, isAmsterdam := range []bool{false, true} {
		rules := rulesForAmsterdam(isAmsterdam)

		// Access list: gas increases with each additional address.
		var prev uint64
		for addrs := 1; addrs <= 5; addrs++ {
			got, err := newListGas(makeAL(addrs), nil, rules)
			require.NoError(t, err)
			require.Greaterf(t, got, prev,
				"gas should increase with more addresses (isAmsterdam=%v addrs=%d)", isAmsterdam, addrs)
			prev = got
		}

		// Auth list: gas increases with each additional authorisation tuple.
		prev = 0
		for n := 1; n <= 5; n++ {
			auth := make([]types.SetCodeAuthorization, n)
			got, err := newListGas(nil, auth, rules)
			require.NoError(t, err)
			require.Greaterf(t, got, prev,
				"gas should increase with more auth tuples (isAmsterdam=%v n=%d)", isAmsterdam, n)
			prev = got
		}
	}
}

// TestNewListGas_AdditiveStorageKeys verifies that each additional storage key
// within an address tuple increases gas by exactly TxAccessListStorageKeyGas
// (plus the Amsterdam surcharge when applicable).
func TestNewListGas_AdditiveStorageKeys(t *testing.T) {
	const (
		keyGas       = gethParams.TxAccessListStorageKeyGas                                               // 1900
		keySurcharge = common.HashLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte // 2048
	)

	for _, isAmsterdam := range []bool{false, true} {
		rules := rulesForAmsterdam(isAmsterdam)
		perKey := uint64(keyGas)
		if isAmsterdam {
			perKey += keySurcharge
		}

		makeALWithKeys := func(keys int) *types.AccessList {
			tup := types.AccessTuple{Address: common.BigToAddress(big.NewInt(1))}
			for k := 0; k < keys; k++ {
				tup.StorageKeys = append(tup.StorageKeys, common.BigToHash(big.NewInt(int64(k+1))))
			}
			al := types.AccessList{tup}
			return &al
		}

		base, err := newListGas(makeALWithKeys(0), nil, rules)
		require.NoError(t, err)

		for keys := 1; keys <= 5; keys++ {
			got, err := newListGas(makeALWithKeys(keys), nil, rules)
			require.NoError(t, err)
			expected := base + uint64(keys)*perKey
			require.Equalf(t, expected, got,
				"isAmsterdam=%v keys=%d", isAmsterdam, keys)
		}
	}
}

// TestNewListGas_CombinedAccessAndAuth verifies that the gas for a combined
// access list + auth list equals the sum of the individual contributions.
func TestNewListGas_CombinedAccessAndAuth(t *testing.T) {
	al := types.AccessList{
		{
			Address:     common.BigToAddress(big.NewInt(1)),
			StorageKeys: []common.Hash{common.BigToHash(big.NewInt(1))},
		},
	}
	alPtr := &al
	auth := make([]types.SetCodeAuthorization, 2)

	for _, isAmsterdam := range []bool{false, true} {
		rules := rulesForAmsterdam(isAmsterdam)

		alOnly, err := newListGas(alPtr, nil, rules)
		require.NoError(t, err)
		authOnly, err := newListGas(nil, auth, rules)
		require.NoError(t, err)
		combined, err := newListGas(alPtr, auth, rules)
		require.NoError(t, err)

		require.Equalf(t, alOnly+authOnly, combined,
			"combined gas should equal sum of individual contributions (isAmsterdam=%v)", isAmsterdam)
	}
}

// TestNewListGas_NilAccessListPointerVsEmpty verifies that a nil access list
// pointer and an empty (non-nil) access list produce identical gas, ensuring the
// nil-guard in newListGas is correct.
func TestNewListGas_NilAccessListPointerVsEmpty(t *testing.T) {
	emptyAL := types.AccessList{}
	emptyALPtr := &emptyAL

	for _, isAmsterdam := range []bool{false, true} {
		rules := rulesForAmsterdam(isAmsterdam)

		withNil, err := newListGas(nil, nil, rules)
		require.NoError(t, err)
		withEmpty, err := newListGas(emptyALPtr, nil, rules)
		require.NoError(t, err)

		require.Equalf(t, withNil, withEmpty,
			"nil and empty access list should produce same gas (isAmsterdam=%v)", isAmsterdam)
	}
}

// TestNewListGas_LargeAccessList is a regression / boundary test that exercises
// a realistic-sized access list (many addresses, many keys) to confirm there are
// no overflow panics and the result is consistent with the per-element costs.
func TestNewListGas_LargeAccessList(t *testing.T) {
	const (
		numAddrs    = 50
		keysPerAddr = 20
		addrGas     = gethParams.TxAccessListAddressGas
		keyGas      = gethParams.TxAccessListStorageKeyGas
	)

	al := types.AccessList{}
	for i := 0; i < numAddrs; i++ {
		tup := types.AccessTuple{Address: common.BigToAddress(big.NewInt(int64(i + 1)))}
		for k := 0; k < keysPerAddr; k++ {
			tup.StorageKeys = append(tup.StorageKeys, common.BigToHash(big.NewInt(int64(k+1))))
		}
		al = append(al, tup)
	}
	alPtr := &al

	// Pre-Amsterdam: straightforward arithmetic
	rulesPreAmst := rulesForAmsterdam(false)
	got, err := newListGas(alPtr, nil, rulesPreAmst)
	require.NoError(t, err)
	expected := uint64(numAddrs)*addrGas + uint64(numAddrs*keysPerAddr)*keyGas
	require.Equal(t, expected, got)

	// Amsterdam: surcharge applies
	const (
		addrSurcharge = common.AddressLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte
		keySurcharge  = common.HashLength * gethParams.TxCostFloorPerToken7976 * gethParams.TxTokenPerNonZeroByte
	)
	rulesAmst := rulesForAmsterdam(true)
	got, err = newListGas(alPtr, nil, rulesAmst)
	require.NoError(t, err)
	expectedAmst := uint64(numAddrs)*(addrGas+addrSurcharge) + uint64(numAddrs*keysPerAddr)*(keyGas+keySurcharge)
	require.Equal(t, expectedAmst, got)
}