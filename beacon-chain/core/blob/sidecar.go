package blob

import (
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	types "github.com/prysmaticlabs/prysm/v3/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v3/crypto/agg_kzg"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	eth "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
)

// ValidateBlobsSidecar verifies the integrity of a sidecar, returning nil if the blob is valid.
// It implements validate_blob_transaction_wrapper in the EIP-4844 spec.
func ValidateBlobsSidecar(slot types.Slot, root [32]byte, commitments [][]byte, sidecar *eth.BlobsSidecar) error {
	if slot != sidecar.BeaconBlockSlot {
		return errors.New("invalid blob slot")
	}
	if root != bytesutil.ToBytes32(sidecar.BeaconBlockRoot) {
		return errors.New("invalid blob beacon block root")
	}

	return agg_kzg.VerifyAggregateKZGProof(commitments, sidecar.Blobs, sidecar.AggregatedProof)
}

func BlockContainsKZGs(b interfaces.BeaconBlock) bool {
	if blocks.IsPreEIP4844Version(b.Version()) {
		return false
	}
	blobKzgs, err := b.Body().BlobKzgs()
	if err != nil {
		// cannot happen!
		return false
	}
	return len(blobKzgs) != 0
}
