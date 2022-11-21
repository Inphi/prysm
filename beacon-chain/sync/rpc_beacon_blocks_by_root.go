package sync

import (
	"context"

	libp2pcore "github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/p2p/types"
	p2ptypes "github.com/prysmaticlabs/prysm/v3/beacon-chain/p2p/types"
	"github.com/prysmaticlabs/prysm/v3/config/params"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	eth "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
	"github.com/sirupsen/logrus"
)

// sendRecentBeaconBlocksRequest sends a recent beacon blocks request to a peer to get
// those corresponding blocks from that peer.
func (s *Service) sendRecentBeaconBlocksRequest(ctx context.Context, blockRoots *p2ptypes.BeaconBlockByRootsReq, id peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, respTimeout)
	defer cancel()

	_, err := SendBeaconBlocksByRootRequest(ctx, s.cfg.chain, s.cfg.p2p, id, blockRoots, func(blk interfaces.SignedBeaconBlock) error {
		blkRoot, err := blk.Block().HashTreeRoot()
		if err != nil {
			return err
		}
		coupledBlk, err := blocks.BuildCoupledBeaconBlock(blk, nil)
		if err != nil {
			return err
		}
		s.pendingQueueLock.Lock()
		if err := s.insertBlockToPendingQueue(blk.Block().Slot(), coupledBlk, blkRoot); err != nil {
			s.pendingQueueLock.Unlock()
			return err
		}
		s.pendingQueueLock.Unlock()
		return nil
	})
	return err
}

// sendRecentBeaconBlocksAndBlobsSidecarsRequest sends a recent beacon blocks request to a peer to get
// those corresponding blocks from that peer.
func (s *Service) sendRecentBeaconBlocksAndBlobsSidecarsRequest(ctx context.Context, blockRoots *p2ptypes.BeaconBlockAndBlobsSidecarByRootsReq, id peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, respTimeout)
	defer cancel()

	_, err := SendBeaconBlocksAndBlobsSidecarsByRootRequest(ctx, s.cfg.chain, s.cfg.p2p, id, blockRoots, func(coupledBlk interfaces.CoupledBeaconBlock) error {
		blk := coupledBlk.UnwrapBlock()
		blkRoot, err := blk.Block().HashTreeRoot()
		if err != nil {
			return err
		}

		s.pendingQueueLock.Lock()
		if err := s.insertBlockToPendingQueue(blk.Block().Slot(), coupledBlk, blkRoot); err != nil {
			s.pendingQueueLock.Unlock()
			return err
		}
		s.pendingQueueLock.Unlock()
		return nil
	})
	return err
}

func (s *Service) blockRootRPCHandler(ctx context.Context, log *logrus.Entry, msg interface{}, stream libp2pcore.Stream, includeBlobsSidecar bool) error {
	ctx, cancel := context.WithTimeout(ctx, ttfbTimeout)
	defer cancel()
	SetRPCStreamDeadlines(stream)

	rawMsg, ok := msg.(*types.BeaconBlockByRootsReq)
	if !ok {
		return errors.New("message is not type BeaconBlockByRootsReq")
	}
	blockRoots := *rawMsg
	if err := s.rateLimiter.validateRequest(stream, uint64(len(blockRoots))); err != nil {
		return err
	}
	if len(blockRoots) == 0 {
		// Add to rate limiter in the event no
		// roots are requested.
		s.rateLimiter.add(stream, 1)
		s.writeErrorResponseToStream(responseCodeInvalidRequest, "no block roots provided in request", stream)
		return errors.New("no block roots provided")
	}

	if uint64(len(blockRoots)) > params.BeaconNetworkConfig().MaxRequestBlocks {
		s.cfg.p2p.Peers().Scorers().BadResponsesScorer().Increment(stream.Conn().RemotePeer())
		s.writeErrorResponseToStream(responseCodeInvalidRequest, "requested more than the max block limit", stream)
		return errors.New("requested more than the max block limit")
	}
	s.rateLimiter.add(stream, int64(len(blockRoots)))

	for _, root := range blockRoots {
		blk, err := s.cfg.beaconDB.Block(ctx, root)
		if err != nil {
			log.WithError(err).Debug("Could not fetch block")
			s.writeErrorResponseToStream(responseCodeServerError, types.ErrGeneric.Error(), stream)
			return err
		}
		if err := blocks.BeaconBlockIsNil(blk); err != nil {
			continue
		}

		if blk.Block().IsBlinded() {
			blk, err = s.cfg.executionPayloadReconstructor.ReconstructFullBellatrixBlock(ctx, blk)
			if err != nil {
				log.WithError(err).Error("Could not get reconstruct full bellatrix block from blinded body")
				s.writeErrorResponseToStream(responseCodeServerError, types.ErrGeneric.Error(), stream)
				return err
			}
		}

		var sidecar *eth.BlobsSidecar
		if includeBlobsSidecar {
			sidecar, err = s.cfg.beaconDB.BlobsSidecar(ctx, root)
			if err != nil {
				log.WithError(err).Debug("Could not fetch blobs sidecar")
				s.writeErrorResponseToStream(responseCodeServerError, types.ErrGeneric.Error(), stream)
				return err
			}
		}

		coupledBlk, err := blocks.BuildCoupledBeaconBlock(blk, sidecar)
		if err != nil {
			log.WithError(err).Debug("Could not build coupled beacon block")
			s.writeErrorResponseToStream(responseCodeServerError, types.ErrGeneric.Error(), stream)
			return err
		}

		if err := s.chunkBlockWriter(stream, coupledBlk); err != nil {
			return err
		}
	}

	closeStream(stream, log)
	return nil
}

// beaconBlocksRootRPCHandler looks up the request blocks from the database from the given block roots.
func (s *Service) beaconBlocksRootRPCHandler(ctx context.Context, msg interface{}, stream libp2pcore.Stream) error {
	log := log.WithField("handler", "beacon_blocks_by_root")
	return s.blockRootRPCHandler(ctx, log, msg, stream, false)
}

// beaconBlocksAndBlobsSidecarsRootRPCHandler looks up the request blocks and blobs sidecars from the database from the given block roots.
func (s *Service) beaconBlocksAndBlobsSidecarsRootRPCHandler(ctx context.Context, msg interface{}, stream libp2pcore.Stream) error {
	log := log.WithField("handler", "beacon_blocks_and_blobs_sidecars_by_root")
	return s.blockRootRPCHandler(ctx, log, msg, stream, true)
}
