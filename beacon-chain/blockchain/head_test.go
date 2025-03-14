package blockchain

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	mock "github.com/prysmaticlabs/prysm/v3/beacon-chain/blockchain/testing"
	testDB "github.com/prysmaticlabs/prysm/v3/beacon-chain/db/testing"
	doublylinkedtree "github.com/prysmaticlabs/prysm/v3/beacon-chain/forkchoice/doubly-linked-tree"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/forkchoice/protoarray"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/state/stategen"
	"github.com/prysmaticlabs/prysm/v3/config/features"
	"github.com/prysmaticlabs/prysm/v3/config/params"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/blocks"
	types "github.com/prysmaticlabs/prysm/v3/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	ethpbv1 "github.com/prysmaticlabs/prysm/v3/proto/eth/v1"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/v3/testing/assert"
	"github.com/prysmaticlabs/prysm/v3/testing/require"
	"github.com/prysmaticlabs/prysm/v3/testing/util"
	"github.com/prysmaticlabs/prysm/v3/time/slots"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func TestSaveHead_Same(t *testing.T) {
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)

	r := [32]byte{'A'}
	service.head = &head{slot: 0, root: r}
	b, err := blocks.NewSignedBeaconBlock(util.NewBeaconBlock())
	require.NoError(t, err)
	st, _ := util.DeterministicGenesisState(t, 1)
	require.NoError(t, service.saveHead(context.Background(), r, b, st))
	assert.Equal(t, types.Slot(0), service.headSlot(), "Head did not stay the same")
	assert.Equal(t, r, service.headRoot(), "Head did not stay the same")
}

func TestSaveHead_Different(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)

	oldBlock := util.SaveBlock(t, context.Background(), service.cfg.BeaconDB, util.NewBeaconBlock())
	oldRoot, err := oldBlock.Block().HashTreeRoot()
	require.NoError(t, err)
	ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	state, blkRoot, err := prepareForkchoiceState(ctx, oldBlock.Block().Slot(), oldRoot, oldBlock.Block().ParentRoot(), [32]byte{}, ojc, ofc)
	require.NoError(t, err)
	require.NoError(t, service.cfg.ForkChoiceStore.InsertNode(ctx, state, blkRoot))
	service.head = &head{
		slot:  0,
		root:  oldRoot,
		block: oldBlock,
	}

	newHeadSignedBlock := util.NewBeaconBlock()
	newHeadSignedBlock.Block.Slot = 1
	newHeadBlock := newHeadSignedBlock.Block

	wsb := util.SaveBlock(t, context.Background(), service.cfg.BeaconDB, newHeadSignedBlock)
	newRoot, err := newHeadBlock.HashTreeRoot()
	require.NoError(t, err)
	state, blkRoot, err = prepareForkchoiceState(ctx, wsb.Block().Slot()-1, wsb.Block().ParentRoot(), service.cfg.ForkChoiceStore.CachedHeadRoot(), [32]byte{}, ojc, ofc)
	require.NoError(t, err)
	require.NoError(t, service.cfg.ForkChoiceStore.InsertNode(ctx, state, blkRoot))

	state, blkRoot, err = prepareForkchoiceState(ctx, wsb.Block().Slot(), newRoot, wsb.Block().ParentRoot(), [32]byte{}, ojc, ofc)
	require.NoError(t, err)
	require.NoError(t, service.cfg.ForkChoiceStore.InsertNode(ctx, state, blkRoot))
	headState, err := util.NewBeaconState()
	require.NoError(t, err)
	require.NoError(t, headState.SetSlot(1))
	require.NoError(t, service.cfg.BeaconDB.SaveStateSummary(context.Background(), &ethpb.StateSummary{Slot: 1, Root: newRoot[:]}))
	require.NoError(t, service.cfg.BeaconDB.SaveState(context.Background(), headState, newRoot))
	require.NoError(t, service.saveHead(context.Background(), newRoot, wsb, headState))

	assert.Equal(t, types.Slot(1), service.HeadSlot(), "Head did not change")

	cachedRoot, err := service.HeadRoot(context.Background())
	require.NoError(t, err)
	assert.DeepEqual(t, cachedRoot, newRoot[:], "Head did not change")
	headBlock, err := service.headBlock()
	require.NoError(t, err)
	pb, err := headBlock.Proto()
	require.NoError(t, err)
	assert.DeepEqual(t, newHeadSignedBlock, pb, "Head did not change")
	assert.DeepSSZEqual(t, headState.CloneInnerState(), service.headState(ctx).CloneInnerState(), "Head did not change")
}

func TestSaveHead_Different_Reorg(t *testing.T) {
	ctx := context.Background()
	hook := logTest.NewGlobal()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)

	oldBlock := util.SaveBlock(t, context.Background(), service.cfg.BeaconDB, util.NewBeaconBlock())
	oldRoot, err := oldBlock.Block().HashTreeRoot()
	require.NoError(t, err)
	ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	state, blkRoot, err := prepareForkchoiceState(ctx, oldBlock.Block().Slot(), oldRoot, oldBlock.Block().ParentRoot(), [32]byte{}, ojc, ofc)
	require.NoError(t, err)
	require.NoError(t, service.cfg.ForkChoiceStore.InsertNode(ctx, state, blkRoot))
	service.head = &head{
		slot:  0,
		root:  oldRoot,
		block: oldBlock,
	}

	reorgChainParent := [32]byte{'B'}
	state, blkRoot, err = prepareForkchoiceState(ctx, 0, reorgChainParent, oldRoot, oldBlock.Block().ParentRoot(), ojc, ofc)
	require.NoError(t, err)
	require.NoError(t, service.cfg.ForkChoiceStore.InsertNode(ctx, state, blkRoot))

	newHeadSignedBlock := util.NewBeaconBlock()
	newHeadSignedBlock.Block.Slot = 1
	newHeadSignedBlock.Block.ParentRoot = reorgChainParent[:]
	newHeadBlock := newHeadSignedBlock.Block

	wsb := util.SaveBlock(t, context.Background(), service.cfg.BeaconDB, newHeadSignedBlock)
	newRoot, err := newHeadBlock.HashTreeRoot()
	require.NoError(t, err)
	state, blkRoot, err = prepareForkchoiceState(ctx, wsb.Block().Slot(), newRoot, wsb.Block().ParentRoot(), [32]byte{}, ojc, ofc)
	require.NoError(t, err)
	require.NoError(t, service.cfg.ForkChoiceStore.InsertNode(ctx, state, blkRoot))
	headState, err := util.NewBeaconState()
	require.NoError(t, err)
	require.NoError(t, headState.SetSlot(1))
	require.NoError(t, service.cfg.BeaconDB.SaveStateSummary(context.Background(), &ethpb.StateSummary{Slot: 1, Root: newRoot[:]}))
	require.NoError(t, service.cfg.BeaconDB.SaveState(context.Background(), headState, newRoot))
	require.NoError(t, service.saveHead(context.Background(), newRoot, wsb, headState))

	assert.Equal(t, types.Slot(1), service.HeadSlot(), "Head did not change")

	cachedRoot, err := service.HeadRoot(context.Background())
	require.NoError(t, err)
	if !bytes.Equal(cachedRoot, newRoot[:]) {
		t.Error("Head did not change")
	}
	headBlock, err := service.headBlock()
	require.NoError(t, err)
	pb, err := headBlock.Proto()
	require.NoError(t, err)
	assert.DeepEqual(t, newHeadSignedBlock, pb, "Head did not change")
	assert.DeepSSZEqual(t, headState.CloneInnerState(), service.headState(ctx).CloneInnerState(), "Head did not change")
	require.LogsContain(t, hook, "Chain reorg occurred")
	require.LogsContain(t, hook, "distance=1")
	require.LogsContain(t, hook, "depth=1")
}

func TestCacheJustifiedStateBalances_CanCache(t *testing.T) {
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)
	ctx := context.Background()

	state, _ := util.DeterministicGenesisState(t, 100)
	r := [32]byte{'a'}
	require.NoError(t, service.cfg.BeaconDB.SaveStateSummary(context.Background(), &ethpb.StateSummary{Root: r[:]}))
	require.NoError(t, service.cfg.BeaconDB.SaveState(context.Background(), state, r))
	balances, err := service.justifiedBalances.get(ctx, r)
	require.NoError(t, err)
	require.DeepEqual(t, balances, state.Balances(), "Incorrect justified balances")
}

func Test_notifyNewHeadEvent(t *testing.T) {
	t.Run("genesis_state_root", func(t *testing.T) {
		bState, _ := util.DeterministicGenesisState(t, 10)
		notifier := &mock.MockStateNotifier{RecordEvents: true}
		srv := &Service{
			cfg: &config{
				StateNotifier: notifier,
			},
			originBlockRoot: [32]byte{1},
		}
		newHeadStateRoot := [32]byte{2}
		newHeadRoot := [32]byte{3}
		err := srv.notifyNewHeadEvent(context.Background(), 1, bState, newHeadStateRoot[:], newHeadRoot[:])
		require.NoError(t, err)
		events := notifier.ReceivedEvents()
		require.Equal(t, 1, len(events))

		eventHead, ok := events[0].Data.(*ethpbv1.EventHead)
		require.Equal(t, true, ok)
		wanted := &ethpbv1.EventHead{
			Slot:                      1,
			Block:                     newHeadRoot[:],
			State:                     newHeadStateRoot[:],
			EpochTransition:           false,
			PreviousDutyDependentRoot: srv.originBlockRoot[:],
			CurrentDutyDependentRoot:  srv.originBlockRoot[:],
		}
		require.DeepSSZEqual(t, wanted, eventHead)
	})
	t.Run("non_genesis_values", func(t *testing.T) {
		bState, _ := util.DeterministicGenesisState(t, 10)
		notifier := &mock.MockStateNotifier{RecordEvents: true}
		genesisRoot := [32]byte{1}
		srv := &Service{
			cfg: &config{
				StateNotifier: notifier,
			},
			originBlockRoot: genesisRoot,
		}
		epoch1Start, err := slots.EpochStart(1)
		require.NoError(t, err)
		epoch2Start, err := slots.EpochStart(1)
		require.NoError(t, err)
		require.NoError(t, bState.SetSlot(epoch1Start))

		newHeadStateRoot := [32]byte{2}
		newHeadRoot := [32]byte{3}
		err = srv.notifyNewHeadEvent(context.Background(), epoch2Start, bState, newHeadStateRoot[:], newHeadRoot[:])
		require.NoError(t, err)
		events := notifier.ReceivedEvents()
		require.Equal(t, 1, len(events))

		eventHead, ok := events[0].Data.(*ethpbv1.EventHead)
		require.Equal(t, true, ok)
		wanted := &ethpbv1.EventHead{
			Slot:                      epoch2Start,
			Block:                     newHeadRoot[:],
			State:                     newHeadStateRoot[:],
			EpochTransition:           true,
			PreviousDutyDependentRoot: genesisRoot[:],
			CurrentDutyDependentRoot:  make([]byte, 32),
		}
		require.DeepSSZEqual(t, wanted, eventHead)
	})
}

func TestSaveOrphanedAtts_NoCommonAncestor_Protoarray(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)
	// this test does not make sense in doubly linked tree since it enforces
	// that the finalized node is a common ancestor
	service.cfg.ForkChoiceStore = protoarray.New(&mockDataAvailability{})

	service.genesisTime = time.Now().Add(time.Duration(-10*int64(1)*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second)

	// Chain setup
	// 0 -- 1 -- 2 -- 3
	//    -4
	st, keys := util.DeterministicGenesisState(t, 64)
	blkG, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 0)
	assert.NoError(t, err)
	util.SaveBlock(t, ctx, service.cfg.BeaconDB, blkG)
	rG, err := blkG.Block.HashTreeRoot()
	require.NoError(t, err)

	blk1, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 1)
	assert.NoError(t, err)
	blk1.Block.ParentRoot = rG[:]
	r1, err := blk1.Block.HashTreeRoot()
	require.NoError(t, err)

	blk2, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 2)
	assert.NoError(t, err)
	blk2.Block.ParentRoot = r1[:]
	r2, err := blk2.Block.HashTreeRoot()
	require.NoError(t, err)

	blk3, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 3)
	assert.NoError(t, err)
	blk3.Block.ParentRoot = r2[:]
	r3, err := blk3.Block.HashTreeRoot()
	require.NoError(t, err)

	blk4 := util.NewBeaconBlock()
	blk4.Block.Slot = 4
	r4, err := blk4.Block.HashTreeRoot()
	require.NoError(t, err)

	for _, blk := range []*ethpb.SignedBeaconBlock{blkG, blk1, blk2, blk3, blk4} {
		r, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
		ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
		state, blkRoot, err := prepareForkchoiceState(ctx, blk.Block.Slot, r, bytesutil.ToBytes32(blk.Block.ParentRoot), [32]byte{}, ojc, ofc)
		require.NoError(t, err)
		require.NoError(t, service.ForkChoicer().InsertNode(ctx, state, blkRoot))
		util.SaveBlock(t, ctx, beaconDB, blk)
	}

	require.NoError(t, service.saveOrphanedAtts(ctx, r3, r4))
	require.Equal(t, 0, service.cfg.AttPool.AggregatedAttestationCount())
}

func TestSaveOrphanedAtts(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)
	service.genesisTime = time.Now().Add(time.Duration(-10*int64(1)*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second)

	// Chain setup
	// 0 -- 1 -- 2 -- 3
	//  \-4
	st, keys := util.DeterministicGenesisState(t, 64)
	blkG, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 0)
	assert.NoError(t, err)

	util.SaveBlock(t, ctx, service.cfg.BeaconDB, blkG)
	rG, err := blkG.Block.HashTreeRoot()
	require.NoError(t, err)

	blk1, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 1)
	assert.NoError(t, err)
	blk1.Block.ParentRoot = rG[:]
	r1, err := blk1.Block.HashTreeRoot()
	require.NoError(t, err)

	blk2, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 2)
	assert.NoError(t, err)
	blk2.Block.ParentRoot = r1[:]
	r2, err := blk2.Block.HashTreeRoot()
	require.NoError(t, err)

	blk3, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 3)
	assert.NoError(t, err)
	blk3.Block.ParentRoot = r2[:]
	r3, err := blk3.Block.HashTreeRoot()
	require.NoError(t, err)

	blk4 := util.NewBeaconBlock()
	blk4.Block.Slot = 4
	blk4.Block.ParentRoot = rG[:]
	r4, err := blk4.Block.HashTreeRoot()
	require.NoError(t, err)
	ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}

	for _, blk := range []*ethpb.SignedBeaconBlock{blkG, blk1, blk2, blk3, blk4} {
		r, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		state, blkRoot, err := prepareForkchoiceState(ctx, blk.Block.Slot, r, bytesutil.ToBytes32(blk.Block.ParentRoot), [32]byte{}, ojc, ofc)
		require.NoError(t, err)
		require.NoError(t, service.ForkChoicer().InsertNode(ctx, state, blkRoot))
		util.SaveBlock(t, ctx, beaconDB, blk)
	}

	require.NoError(t, service.saveOrphanedAtts(ctx, r3, r4))
	require.Equal(t, 3, service.cfg.AttPool.AggregatedAttestationCount())
	wantAtts := []*ethpb.Attestation{
		blk3.Block.Body.Attestations[0],
		blk2.Block.Body.Attestations[0],
		blk1.Block.Body.Attestations[0],
	}
	atts := service.cfg.AttPool.AggregatedAttestations()
	sort.Slice(atts, func(i, j int) bool {
		return atts[i].Data.Slot > atts[j].Data.Slot
	})
	require.DeepEqual(t, wantAtts, atts)
}

func TestSaveOrphanedAtts_CanFilter(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)
	service.genesisTime = time.Now().Add(time.Duration(-1*int64(params.BeaconConfig().SlotsPerEpoch+2)*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second)

	// Chain setup
	// 0 -- 1 -- 2
	//  \-4
	st, keys := util.DeterministicGenesisState(t, 64)
	blkG, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 0)
	assert.NoError(t, err)
	util.SaveBlock(t, ctx, service.cfg.BeaconDB, blkG)
	rG, err := blkG.Block.HashTreeRoot()
	require.NoError(t, err)

	blk1, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 1)
	assert.NoError(t, err)
	blk1.Block.ParentRoot = rG[:]
	r1, err := blk1.Block.HashTreeRoot()
	require.NoError(t, err)

	blk2, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 2)
	assert.NoError(t, err)
	blk2.Block.ParentRoot = r1[:]
	r2, err := blk2.Block.HashTreeRoot()
	require.NoError(t, err)

	blk4 := util.NewBeaconBlock()
	blk4.Block.Slot = 4
	blk4.Block.ParentRoot = rG[:]
	r4, err := blk4.Block.HashTreeRoot()
	require.NoError(t, err)
	ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}

	for _, blk := range []*ethpb.SignedBeaconBlock{blkG, blk1, blk2, blk4} {
		r, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		state, blkRoot, err := prepareForkchoiceState(ctx, blk.Block.Slot, r, bytesutil.ToBytes32(blk.Block.ParentRoot), [32]byte{}, ojc, ofc)
		require.NoError(t, err)
		require.NoError(t, service.ForkChoicer().InsertNode(ctx, state, blkRoot))
		util.SaveBlock(t, ctx, beaconDB, blk)
	}

	require.NoError(t, service.saveOrphanedAtts(ctx, r2, r4))
	require.Equal(t, 0, service.cfg.AttPool.AggregatedAttestationCount())
}

func TestSaveOrphanedAtts_DoublyLinkedTrie(t *testing.T) {
	resetCfg := features.InitWithReset(&features.Flags{
		DisableForkchoiceDoublyLinkedTree: false,
	})
	defer resetCfg()

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)
	service.genesisTime = time.Now().Add(time.Duration(-10*int64(1)*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second)

	// Chain setup
	// 0 -- 1 -- 2 -- 3
	//  \-4
	st, keys := util.DeterministicGenesisState(t, 64)
	blkG, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 0)
	assert.NoError(t, err)
	util.SaveBlock(t, ctx, service.cfg.BeaconDB, blkG)
	rG, err := blkG.Block.HashTreeRoot()
	require.NoError(t, err)

	blk1, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 1)
	assert.NoError(t, err)
	blk1.Block.ParentRoot = rG[:]
	r1, err := blk1.Block.HashTreeRoot()
	require.NoError(t, err)

	blk2, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 2)
	assert.NoError(t, err)
	blk2.Block.ParentRoot = r1[:]
	r2, err := blk2.Block.HashTreeRoot()
	require.NoError(t, err)

	blk3, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 3)
	assert.NoError(t, err)
	blk3.Block.ParentRoot = r2[:]
	r3, err := blk3.Block.HashTreeRoot()
	require.NoError(t, err)

	blk4 := util.NewBeaconBlock()
	blk4.Block.Slot = 4
	blk4.Block.ParentRoot = rG[:]
	r4, err := blk4.Block.HashTreeRoot()
	require.NoError(t, err)

	ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	for _, blk := range []*ethpb.SignedBeaconBlock{blkG, blk1, blk2, blk3, blk4} {
		r, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		state, blkRoot, err := prepareForkchoiceState(ctx, blk.Block.Slot, r, bytesutil.ToBytes32(blk.Block.ParentRoot), [32]byte{}, ojc, ofc)
		require.NoError(t, err)
		require.NoError(t, service.ForkChoicer().InsertNode(ctx, state, blkRoot))
		util.SaveBlock(t, ctx, beaconDB, blk)
	}

	require.NoError(t, service.saveOrphanedAtts(ctx, r3, r4))
	require.Equal(t, 3, service.cfg.AttPool.AggregatedAttestationCount())
	wantAtts := []*ethpb.Attestation{
		blk3.Block.Body.Attestations[0],
		blk2.Block.Body.Attestations[0],
		blk1.Block.Body.Attestations[0],
	}
	atts := service.cfg.AttPool.AggregatedAttestations()
	sort.Slice(atts, func(i, j int) bool {
		return atts[i].Data.Slot > atts[j].Data.Slot
	})
	require.DeepEqual(t, wantAtts, atts)
}

func TestSaveOrphanedAtts_CanFilter_DoublyLinkedTrie(t *testing.T) {
	resetCfg := features.InitWithReset(&features.Flags{
		DisableForkchoiceDoublyLinkedTree: false,
	})
	defer resetCfg()

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	service := setupBeaconChain(t, beaconDB)
	service.genesisTime = time.Now().Add(time.Duration(-1*int64(params.BeaconConfig().SlotsPerEpoch+2)*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second)

	// Chain setup
	// 0 -- 1 -- 2
	//  \-4
	st, keys := util.DeterministicGenesisState(t, 64)
	blkG, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 0)
	assert.NoError(t, err)
	util.SaveBlock(t, ctx, service.cfg.BeaconDB, blkG)
	rG, err := blkG.Block.HashTreeRoot()
	require.NoError(t, err)

	blk1, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 1)
	assert.NoError(t, err)
	blk1.Block.ParentRoot = rG[:]
	r1, err := blk1.Block.HashTreeRoot()
	require.NoError(t, err)

	blk2, err := util.GenerateFullBlock(st, keys, util.DefaultBlockGenConfig(), 2)
	assert.NoError(t, err)
	blk2.Block.ParentRoot = r1[:]
	r2, err := blk2.Block.HashTreeRoot()
	require.NoError(t, err)

	blk4 := util.NewBeaconBlock()
	blk4.Block.Slot = 4
	blk4.Block.ParentRoot = rG[:]
	r4, err := blk4.Block.HashTreeRoot()
	require.NoError(t, err)

	ojc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	ofc := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	for _, blk := range []*ethpb.SignedBeaconBlock{blkG, blk1, blk2, blk4} {
		r, err := blk.Block.HashTreeRoot()
		require.NoError(t, err)
		state, blkRoot, err := prepareForkchoiceState(ctx, blk.Block.Slot, r, bytesutil.ToBytes32(blk.Block.ParentRoot), [32]byte{}, ojc, ofc)
		require.NoError(t, err)
		require.NoError(t, service.ForkChoicer().InsertNode(ctx, state, blkRoot))
		util.SaveBlock(t, ctx, beaconDB, blk)
	}

	require.NoError(t, service.saveOrphanedAtts(ctx, r2, r4))
	require.Equal(t, 0, service.cfg.AttPool.AggregatedAttestationCount())
}

func TestUpdateHead_noSavedChanges(t *testing.T) {
	ctx := context.Background()

	beaconDB := testDB.SetupDB(t)
	fcs := doublylinkedtree.New(&mockDataAvailability{})
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}

	service, err := NewService(ctx, opts...)
	require.NoError(t, err)
	ojp := &ethpb.Checkpoint{Root: params.BeaconConfig().ZeroHash[:]}
	st, blkRoot, err := prepareForkchoiceState(ctx, 0, [32]byte{}, [32]byte{}, [32]byte{}, ojp, ojp)
	require.NoError(t, err)
	require.NoError(t, fcs.InsertNode(ctx, st, blkRoot))

	bellatrixBlk := util.SaveBlock(t, ctx, beaconDB, util.NewBeaconBlockBellatrix())
	bellatrixBlkRoot, err := bellatrixBlk.Block().HashTreeRoot()
	require.NoError(t, err)
	fcp := &ethpb.Checkpoint{
		Root:  bellatrixBlkRoot[:],
		Epoch: 0,
	}
	require.NoError(t, beaconDB.SaveGenesisBlockRoot(ctx, bellatrixBlkRoot))

	bellatrixState, _ := util.DeterministicGenesisStateBellatrix(t, 2)
	require.NoError(t, beaconDB.SaveState(ctx, bellatrixState, bellatrixBlkRoot))
	service.cfg.StateGen.SaveFinalizedState(0, bellatrixBlkRoot, bellatrixState)

	headRoot := service.headRoot()
	require.Equal(t, [32]byte{}, headRoot)

	st, blkRoot, err = prepareForkchoiceState(ctx, 0, bellatrixBlkRoot, [32]byte{}, [32]byte{}, fcp, fcp)
	require.NoError(t, err)
	require.NoError(t, fcs.InsertNode(ctx, st, blkRoot))
	newRoot, err := service.cfg.ForkChoiceStore.Head(ctx, []uint64{1, 2})
	require.NoError(t, err)
	require.NotEqual(t, headRoot, newRoot)
	require.Equal(t, headRoot, service.headRoot())
}
