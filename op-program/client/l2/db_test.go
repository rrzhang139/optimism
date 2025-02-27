package l2

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

var (
	userAccount    = common.HexToAddress("0x1111")
	codeAccount    = common.HexToAddress("0x2222")
	unknownAccount = common.HexToAddress("0x3333")
)

// Should implement the KeyValueStore API
var _ ethdb.KeyValueStore = (*OracleKeyValueStore)(nil)

func TestGet(t *testing.T) {
	t.Run("IncorrectLengthKey", func(t *testing.T) {
		oracle := newStubStateOracle(t)
		db := NewOracleBackedDB(oracle)
		val, err := db.Get([]byte{1, 2, 3})
		require.ErrorIs(t, err, ErrInvalidKeyLength)
		require.Nil(t, val)
	})

	t.Run("KeyWithCodePrefix", func(t *testing.T) {
		oracle := newStubStateOracle(t)
		db := NewOracleBackedDB(oracle)
		key := common.HexToHash("0x12345678")
		prefixedKey := append(rawdb.CodePrefix, key.Bytes()...)

		expected := []byte{1, 2, 3}
		oracle.code[key] = expected
		val, err := db.Get(prefixedKey)

		require.NoError(t, err)
		require.Equal(t, expected, val)
	})

	t.Run("NormalKeyThatHappensToStartWithCodePrefix", func(t *testing.T) {
		oracle := newStubStateOracle(t)
		db := NewOracleBackedDB(oracle)
		key := make([]byte, common.HashLength)
		copy(rawdb.CodePrefix, key)
		println(key[0])
		expected := []byte{1, 2, 3}
		oracle.data[common.BytesToHash(key)] = expected
		val, err := db.Get(key)

		require.NoError(t, err)
		require.Equal(t, expected, val)
	})

	t.Run("KnownKey", func(t *testing.T) {
		key := common.HexToHash("0xAA4488")
		expected := []byte{2, 6, 3, 8}
		oracle := newStubStateOracle(t)
		oracle.data[key] = expected
		db := NewOracleBackedDB(oracle)
		val, err := db.Get(key.Bytes())
		require.NoError(t, err)
		require.Equal(t, expected, val)
	})
}

func TestPut(t *testing.T) {
	t.Run("NewKey", func(t *testing.T) {
		oracle := newStubStateOracle(t)
		db := NewOracleBackedDB(oracle)
		key := common.HexToHash("0xAA4488")
		value := []byte{2, 6, 3, 8}
		err := db.Put(key.Bytes(), value)
		require.NoError(t, err)

		actual, err := db.Get(key.Bytes())
		require.NoError(t, err)
		require.Equal(t, value, actual)
	})
	t.Run("ReplaceKey", func(t *testing.T) {
		oracle := newStubStateOracle(t)
		db := NewOracleBackedDB(oracle)
		key := common.HexToHash("0xAA4488")
		value1 := []byte{2, 6, 3, 8}
		value2 := []byte{1, 2, 3}
		err := db.Put(key.Bytes(), value1)
		require.NoError(t, err)
		err = db.Put(key.Bytes(), value2)
		require.NoError(t, err)

		actual, err := db.Get(key.Bytes())
		require.NoError(t, err)
		require.Equal(t, value2, actual)
	})
}

func TestSupportsStateDBOperations(t *testing.T) {
	l2Genesis := createGenesis()
	realDb := rawdb.NewDatabase(memorydb.New())
	genesisBlock := l2Genesis.MustCommit(realDb)

	loader := &kvStateOracle{
		t:      t,
		source: realDb,
	}
	assertStateDataAvailable(t, NewOracleBackedDB(loader), l2Genesis, genesisBlock)
}

func TestUpdateState(t *testing.T) {
	l2Genesis := createGenesis()
	oracle := newStubStateOracle(t)
	db := rawdb.NewDatabase(NewOracleBackedDB(oracle))

	genesisBlock := l2Genesis.MustCommit(db)
	assertStateDataAvailable(t, db, l2Genesis, genesisBlock)

	statedb, err := state.New(genesisBlock.Root(), state.NewDatabase(rawdb.NewDatabase(db)), nil)
	require.NoError(t, err)
	statedb.SetBalance(userAccount, big.NewInt(50))
	require.Equal(t, big.NewInt(50), statedb.GetBalance(userAccount))
	statedb.SetNonce(userAccount, uint64(5))
	require.Equal(t, uint64(5), statedb.GetNonce(userAccount))

	statedb.SetBalance(unknownAccount, big.NewInt(60))
	require.Equal(t, big.NewInt(60), statedb.GetBalance(unknownAccount))
	statedb.SetCode(codeAccount, []byte{1})
	require.Equal(t, []byte{1}, statedb.GetCode(codeAccount))

	// Changes should be available under the new state root after committing
	newRoot, err := statedb.Commit(false)
	require.NoError(t, err)
	err = statedb.Database().TrieDB().Commit(newRoot, true)
	require.NoError(t, err)

	statedb, err = state.New(newRoot, state.NewDatabase(rawdb.NewDatabase(db)), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(50), statedb.GetBalance(userAccount))
	require.Equal(t, uint64(5), statedb.GetNonce(userAccount))
	require.Equal(t, big.NewInt(60), statedb.GetBalance(unknownAccount))
	require.Equal(t, []byte{1}, statedb.GetCode(codeAccount))
}

func createGenesis() *core.Genesis {
	l2Genesis := &core.Genesis{
		Config:     &params.ChainConfig{},
		Difficulty: common.Big0,
		ParentHash: common.Hash{},
		BaseFee:    big.NewInt(7),
		Alloc: map[common.Address]core.GenesisAccount{
			userAccount: {
				Balance: big.NewInt(1_000_000_000_000_000_000),
				Nonce:   10,
			},
			codeAccount: {
				Balance: big.NewInt(100),
				Code:    []byte{5, 7, 8, 3, 4},
				Storage: map[common.Hash]common.Hash{
					common.HexToHash("0x01"): common.HexToHash("0x11"),
					common.HexToHash("0x02"): common.HexToHash("0x12"),
					common.HexToHash("0x03"): common.HexToHash("0x13"),
				},
			},
		},
	}
	return l2Genesis
}

func assertStateDataAvailable(t *testing.T, db ethdb.KeyValueStore, l2Genesis *core.Genesis, genesisBlock *types.Block) {
	statedb, err := state.New(genesisBlock.Root(), state.NewDatabase(rawdb.NewDatabase(db)), nil)
	require.NoError(t, err)

	for address, account := range l2Genesis.Alloc {
		require.Equal(t, account.Balance, statedb.GetBalance(address))
		require.Equal(t, account.Nonce, statedb.GetNonce(address))
		require.Equal(t, common.BytesToHash(crypto.Keccak256(account.Code)), statedb.GetCodeHash(address))
		require.Equal(t, account.Code, statedb.GetCode(address))
		for key, value := range account.Storage {
			require.Equal(t, value, statedb.GetState(address, key))
		}
	}
	require.Equal(t, common.Hash{}, statedb.GetState(codeAccount, common.HexToHash("0x99")), "retrieve unset storage key")
	require.Equal(t, common.Big0, statedb.GetBalance(unknownAccount), "unset account balance")
	require.Equal(t, uint64(0), statedb.GetNonce(unknownAccount), "unset account balance")
	require.Nil(t, statedb.GetCode(unknownAccount), "unset account code")
	require.Equal(t, common.Hash{}, statedb.GetCodeHash(unknownAccount), "unset account code hash")
}

func newStubStateOracle(t *testing.T) *stubStateOracle {
	return &stubStateOracle{
		t:    t,
		data: make(map[common.Hash][]byte),
		code: make(map[common.Hash][]byte),
	}
}

type stubStateOracle struct {
	t    *testing.T
	data map[common.Hash][]byte
	code map[common.Hash][]byte
}

func (o *stubStateOracle) NodeByHash(nodeHash common.Hash) []byte {
	data, ok := o.data[nodeHash]
	if !ok {
		o.t.Fatalf("no value for node %v", nodeHash)
	}
	return data
}

func (o *stubStateOracle) CodeByHash(hash common.Hash) []byte {
	data, ok := o.code[hash]
	if !ok {
		o.t.Fatalf("no value for code %v", hash)
	}
	return data
}

// kvStateOracle loads data from a source ethdb.KeyValueStore
type kvStateOracle struct {
	t      *testing.T
	source ethdb.KeyValueStore
}

func (o *kvStateOracle) NodeByHash(nodeHash common.Hash) []byte {
	val, err := o.source.Get(nodeHash.Bytes())
	if err != nil {
		o.t.Fatalf("error retrieving node %v: %v", nodeHash, err)
	}
	return val
}

func (o *kvStateOracle) CodeByHash(hash common.Hash) []byte {
	return rawdb.ReadCode(o.source, hash)
}
