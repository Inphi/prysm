package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	testDB "github.com/prysmaticlabs/prysm/v3/beacon-chain/db/testing"
	mocks "github.com/prysmaticlabs/prysm/v3/beacon-chain/execution/testing"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/forkchoice/protoarray"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/state/stategen"
	"github.com/prysmaticlabs/prysm/v3/config/params"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	enginev1 "github.com/prysmaticlabs/prysm/v3/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/v3/testing/require"
)

func Test_validTerminalPowBlock(t *testing.T) {
	tests := []struct {
		name              string
		currentDifficulty *uint256.Int
		parentDifficulty  *uint256.Int
		ttd               uint64
		want              bool
	}{
		{
			name:              "current > ttd, parent > ttd",
			currentDifficulty: uint256.NewInt(2),
			parentDifficulty:  uint256.NewInt(2),
			ttd:               1,
			want:              false,
		},
		{
			name:              "current < ttd, parent < ttd",
			currentDifficulty: uint256.NewInt(2),
			parentDifficulty:  uint256.NewInt(2),
			ttd:               3,
			want:              false,
		},
		{
			name:              "current == ttd, parent == ttd",
			currentDifficulty: uint256.NewInt(2),
			parentDifficulty:  uint256.NewInt(2),
			ttd:               2,
			want:              false,
		},
		{
			name:              "current > ttd, parent == ttd",
			currentDifficulty: uint256.NewInt(2),
			parentDifficulty:  uint256.NewInt(1),
			ttd:               1,
			want:              false,
		},
		{
			name:              "current == ttd, parent < ttd",
			currentDifficulty: uint256.NewInt(2),
			parentDifficulty:  uint256.NewInt(1),
			ttd:               2,
			want:              true,
		},
		{
			name:              "current > ttd, parent < ttd",
			currentDifficulty: uint256.NewInt(3),
			parentDifficulty:  uint256.NewInt(1),
			ttd:               2,
			want:              true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := params.BeaconConfig()
			cfg.TerminalTotalDifficulty = fmt.Sprint(tt.ttd)
			params.OverrideBeaconConfig(cfg)
			got, err := validateTerminalBlockDifficulties(tt.currentDifficulty, tt.parentDifficulty)
			require.NoError(t, err)
			if got != tt.want {
				t.Errorf("validateTerminalBlockDifficulties() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_validTerminalPowBlockSpecConfig(t *testing.T) {
	cfg := params.BeaconConfig()
	cfg.TerminalTotalDifficulty = "115792089237316195423570985008687907853269984665640564039457584007913129638912"
	params.OverrideBeaconConfig(cfg)

	i, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129638912", 10)
	current, of := uint256.FromBig(i)
	require.Equal(t, of, false)
	i, _ = new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129638911", 10)
	parent, of := uint256.FromBig(i)
	require.Equal(t, of, false)

	got, err := validateTerminalBlockDifficulties(current, parent)
	require.NoError(t, err)
	require.Equal(t, true, got)
}

func Test_validateMergeBlock(t *testing.T) {
	cfg := params.BeaconConfig()
	cfg.TerminalTotalDifficulty = "2"
	params.OverrideBeaconConfig(cfg)

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	fcs := protoarray.New(nil)
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)

	engine := &mocks.EngineClient{BlockByHashMap: map[[32]byte]*enginev1.ExecutionBlock{}}
	service.cfg.ExecutionEngineCaller = engine
	a := [32]byte{'a'}
	b := [32]byte{'b'}
	mergeBlockParentHash := [32]byte{'3'}
	engine.BlockByHashMap[a] = &enginev1.ExecutionBlock{
		Header: gethtypes.Header{
			ParentHash: b,
		},
		TotalDifficulty: "0x2",
	}
	engine.BlockByHashMap[b] = &enginev1.ExecutionBlock{
		Header: gethtypes.Header{
			ParentHash: mergeBlockParentHash,
		},
		TotalDifficulty: "0x1",
	}
	blk := &ethpb.SignedBeaconBlockBellatrix{
		Block: &ethpb.BeaconBlockBellatrix{
			Slot: 1,
			Body: &ethpb.BeaconBlockBodyBellatrix{
				ExecutionPayload: &enginev1.ExecutionPayload{
					ParentHash: a[:],
				},
			},
		},
	}
	bk, err := blocks.NewSignedBeaconBlock(blk)
	require.NoError(t, err)
	require.NoError(t, service.validateMergeBlock(ctx, bk))

	cfg.TerminalTotalDifficulty = "1"
	params.OverrideBeaconConfig(cfg)
	err = service.validateMergeBlock(ctx, bk)
	require.ErrorContains(t, "invalid TTD, configTTD: 1, currentTTD: 2, parentTTD: 1", err)
	require.Equal(t, true, IsInvalidBlock(err))
}

func Test_getBlkParentHashAndTD(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	fcs := protoarray.New(nil)
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)

	engine := &mocks.EngineClient{BlockByHashMap: map[[32]byte]*enginev1.ExecutionBlock{}}
	service.cfg.ExecutionEngineCaller = engine
	h := [32]byte{'a'}
	p := [32]byte{'b'}
	td := "0x1"
	engine.BlockByHashMap[h] = &enginev1.ExecutionBlock{
		Header: gethtypes.Header{
			ParentHash: p,
		},
		TotalDifficulty: td,
	}
	parentHash, totalDifficulty, err := service.getBlkParentHashAndTD(ctx, h[:])
	require.NoError(t, err)
	require.Equal(t, p, bytesutil.ToBytes32(parentHash))
	require.Equal(t, td, totalDifficulty.String())

	_, _, err = service.getBlkParentHashAndTD(ctx, []byte{'c'})
	require.ErrorContains(t, "could not get pow block: block not found", err)

	engine.BlockByHashMap[h] = nil
	_, _, err = service.getBlkParentHashAndTD(ctx, h[:])
	require.ErrorContains(t, "pow block is nil", err)

	engine.BlockByHashMap[h] = &enginev1.ExecutionBlock{
		Header: gethtypes.Header{
			ParentHash: p,
		},
		TotalDifficulty: "1",
	}
	_, _, err = service.getBlkParentHashAndTD(ctx, h[:])
	require.ErrorContains(t, "could not decode merge block total difficulty: hex string without 0x prefix", err)

	engine.BlockByHashMap[h] = &enginev1.ExecutionBlock{
		Header: gethtypes.Header{
			ParentHash: p,
		},
		TotalDifficulty: "0XFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
	}
	_, _, err = service.getBlkParentHashAndTD(ctx, h[:])
	require.ErrorContains(t, "could not decode merge block total difficulty: hex number > 256 bits", err)
}

func Test_validateTerminalBlockHash(t *testing.T) {
	wrapped, err := blocks.NewExecutionData(&enginev1.ExecutionPayload{})
	require.NoError(t, err)
	require.NoError(t, validateTerminalBlockHash(1, wrapped))

	cfg := params.BeaconConfig()
	cfg.TerminalBlockHash = [32]byte{0x01}
	params.OverrideBeaconConfig(cfg)
	require.ErrorContains(t, "terminal block hash activation epoch not reached", validateTerminalBlockHash(1, wrapped))

	cfg.TerminalBlockHashActivationEpoch = 0
	params.OverrideBeaconConfig(cfg)
	require.ErrorContains(t, "parent hash does not match terminal block hash", validateTerminalBlockHash(1, wrapped))

	wrapped, err = blocks.NewExecutionData(&enginev1.ExecutionPayload{
		ParentHash: cfg.TerminalBlockHash.Bytes(),
	})
	require.NoError(t, err)
	require.NoError(t, validateTerminalBlockHash(1, wrapped))
}
