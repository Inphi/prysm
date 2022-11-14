package sync

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"github.com/protolambda/go-kzg/bls"
	kbls "github.com/protolambda/go-kzg/bls"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	enginev1 "github.com/prysmaticlabs/prysm/v3/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
	"go.opencensus.io/trace"
)

// [IGNORE] the `sidecar.beacon_block_slot` is for the current slot (with a `MAXIMUM_GOSSIP_CLOCK_DISPARITY` allowance) -- i.e. `sidecar.beacon_block_slot == block.slot`.
// [REJECT] the `sidecar.blobs` are all well formatted, i.e. the `BLSFieldElement` in valid range (`x < BLS_MODULUS`).
// [REJECT] The KZG proof is a correctly encoded compressed BLS G1 Point -- i.e. `bls.KeyValidate(blobs_sidecar.kzg_aggregated_proof)`
func (s *Service) validateBlobsSidecar(ctx context.Context, blk interfaces.SignedBeaconBlock, sidecar *ethpb.BlobsSidecar) (pubsub.ValidationResult, error) {
	ctx, span := trace.StartSpan(ctx, "sync.validateBlobsSidecar")
	defer span.End()

	// Assuming this is a coupled beacon block
	if sidecar.BeaconBlockSlot != blk.Block().Slot() {
		return pubsub.ValidationIgnore, errors.New("beacon block slot does not equal blobs sidecar slot")
	}
	if err := validateBlobFr(sidecar.Blobs); err != nil {
		log.WithError(err).WithField("slot", sidecar.BeaconBlockSlot).Debug("Sidecar contains invalid BLS field elements")
		return pubsub.ValidationReject, err
	}
	if _, err := bls.FromCompressedG1(sidecar.AggregatedProof); err != nil {
		return pubsub.ValidationReject, err
	}
	return pubsub.ValidationAccept, nil
}

func validateBlobFr(blobs []*enginev1.Blob) error {
	for _, blob := range blobs {
		for _, b := range blob.Blob {
			if len(b) != 32 {
				return errors.New("invalid blob field element size")
			}
			if !kbls.ValidFr(bytesutil.ToBytes32(b)) {
				return errors.New("invalid blob field element")
			}
		}
	}
	return nil
}
