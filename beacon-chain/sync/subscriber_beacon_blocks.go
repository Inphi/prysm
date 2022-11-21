package sync

import (
	"context"
	"fmt"

	"github.com/prysmaticlabs/prysm/v3/beacon-chain/blockchain"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/core/transition/interop"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) beaconBlockSubscriber(ctx context.Context, msg proto.Message) error {
	signed, err := blocks.NewSignedBeaconBlock(msg)
	if err != nil {
		return err
	}
	coupledBlk, err := blocks.BuildCoupledBeaconBlock(signed, nil)
	if err != nil {
		return err
	}
	return s.receiveBeaconBlock(ctx, coupledBlk)
}

func (s *Service) beaconBlockAndBlobsSidecarSubscriber(ctx context.Context, msg proto.Message) error {
	m, ok := msg.(*ethpb.SignedBeaconBlockAndBlobsSidecar)
	if !ok {
		return fmt.Errorf("message was not type *eth.SignedBeaconBlockAndBlobsSidecar, type=%T", msg)
	}
	signed, err := blocks.NewSignedBeaconBlock(m.BeaconBlock)
	if err != nil {
		return err
	}
	coupledBlk, err := blocks.BuildCoupledBeaconBlock(signed, m.BlobsSidecar)
	if err != nil {
		return err
	}
	return s.receiveBeaconBlock(ctx, coupledBlk)
}

func (s *Service) receiveBeaconBlock(ctx context.Context, coupledBlk interfaces.CoupledBeaconBlock) error {
	signed := coupledBlk.UnwrapBlock()
	if err := blocks.BeaconBlockIsNil(signed); err != nil {
		return err
	}

	s.setSeenBlockIndexSlot(signed.Block().Slot(), signed.Block().ProposerIndex())

	block := signed.Block()

	root, err := block.HashTreeRoot()
	if err != nil {
		return err
	}

	if err := s.cfg.chain.ReceiveBlock(ctx, coupledBlk, root); err != nil {
		if blockchain.IsInvalidBlock(err) {
			r := blockchain.InvalidBlockRoot(err)
			if r != [32]byte{} {
				s.setBadBlock(ctx, r) // Setting head block as bad.
			} else {
				interop.WriteBlockToDisk(signed, true /*failed*/)
				s.setBadBlock(ctx, root)
			}
		}
		// Set the returned invalid ancestors as bad.
		for _, root := range blockchain.InvalidAncestorRoots(err) {
			s.setBadBlock(ctx, root)
		}
		return err
	}
	return err
}

// The input attestations are seen by the network, this deletes them from pool
// so proposers don't include them in a block for the future.
func (s *Service) deleteAttsInPool(atts []*ethpb.Attestation) error {
	for _, att := range atts {
		if helpers.IsAggregated(att) {
			if err := s.cfg.attPool.DeleteAggregatedAttestation(att); err != nil {
				return err
			}
		} else {
			// Ideally there's shouldn't be any unaggregated attestation in the block.
			if err := s.cfg.attPool.DeleteUnaggregatedAttestation(att); err != nil {
				return err
			}
		}
	}
	return nil
}
