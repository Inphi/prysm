package blob

import (
	"github.com/protolambda/go-kzg/eth"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	types "github.com/prysmaticlabs/prysm/v3/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	v1 "github.com/prysmaticlabs/prysm/v3/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
)

type commitmentSequenceImpl [][]byte

func (s commitmentSequenceImpl) At(i int) eth.KZGCommitment {
	var out eth.KZGCommitment
	copy(out[:], s[i])
	return out
}

func (s commitmentSequenceImpl) Len() int {
	return len(s)
}

type BlobImpl [][]byte

func (b BlobImpl) At(i int) [32]byte {
	var out [32]byte
	copy(out[:], b[i])
	return out
}

func (b BlobImpl) Len() int {
	return len(b)
}

type BlobsSequenceImpl []*v1.Blob

func (s BlobsSequenceImpl) At(i int) eth.Blob {
	return BlobImpl(s[i].Blob)
}

func (s BlobsSequenceImpl) Len() int {
	return len(s)
}

// ValidateBlobsSidecar verifies the integrity of a sidecar, returning nil if the blob is valid.
func ValidateBlobsSidecar(slot types.Slot, root [32]byte, commitments [][]byte, sidecar *ethpb.BlobsSidecar) error {
	kzgSidecar := eth.BlobsSidecar{
		BeaconBlockRoot:    eth.Root(bytesutil.ToBytes32(sidecar.BeaconBlockRoot)),
		BeaconBlockSlot:    eth.Slot(sidecar.BeaconBlockSlot),
		Blobs:              BlobsSequenceImpl(sidecar.Blobs),
		KZGAggregatedProof: eth.KZGProof(bytesutil.ToBytes48(sidecar.AggregatedProof)),
	}
	return eth.ValidateBlobsSidecar(eth.Slot(slot), eth.Root(root), commitmentSequenceImpl(commitments), kzgSidecar)
}

func BlockContainsKZGs(b interfaces.BeaconBlock) bool {
	if blocks.IsPreEIP4844Version(b.Version()) {
		return false
	}
	blobKzgs, err := b.Body().BlobKzgCommitments()
	if err != nil {
		// cannot happen!
		return false
	}
	return len(blobKzgs) != 0
}
