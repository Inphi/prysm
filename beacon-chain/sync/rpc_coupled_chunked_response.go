package sync

import (
	libp2pcore "github.com/libp2p/go-libp2p/core"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/blockchain"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/p2p"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	enginev1 "github.com/prysmaticlabs/prysm/v3/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
)

// ReadChunkedCoupledBlock handles each response chunk that is sent by the
// peer and converts it into a beacon block and blobs sidecar.
func ReadChunkedCoupledBlock(stream libp2pcore.Stream, chain blockchain.ForkFetcher, p2p p2p.EncodingProvider, isFirstChunk bool) (interfaces.CoupledBeaconBlock, error) {
	// Handle deadlines differently for first chunk
	if isFirstChunk {
		return readFirstChunkedCoupledBlock(stream, chain, p2p)
	}

	return readCoupledResponseChunk(stream, chain, p2p)
}

func readFirstChunkedCoupledBlock(stream libp2pcore.Stream, chain blockchain.ForkFetcher, p2p p2p.EncodingProvider) (interfaces.CoupledBeaconBlock, error) {
	code, errMsg, err := ReadStatusCode(stream, p2p.Encoding())
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errors.New(errMsg)
	}
	rpcCtx, err := readContextFromStream(stream, chain)
	if err != nil {
		return nil, err
	}
	// beacon_block_and_blobs_sidecar uses v1
	if len(rpcCtx) != 0 {
		return nil, errors.New("unexpected fork digest in stream")
	}
	blk, err := coupledBlockDataType()
	if err != nil {
		return nil, err
	}
	err = p2p.Encoding().DecodeWithMaxLength(stream, blk)
	return blk, err
}

// readCoupledResponseChunk reads the response from the stream and decodes it into the
// provided message type.
func readCoupledResponseChunk(stream libp2pcore.Stream, chain blockchain.ForkFetcher, p2p p2p.EncodingProvider) (interfaces.CoupledBeaconBlock, error) {
	SetStreamReadDeadline(stream, respTimeout)
	code, errMsg, err := readStatusCodeNoDeadline(stream, p2p.Encoding())
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errors.New(errMsg)
	}
	// No-op for now with the rpc context.
	rpcCtx, err := readContextFromStream(stream, chain)
	if err != nil {
		return nil, err
	}
	// beacon_block_and_blobs_sidecar uses v1
	if len(rpcCtx) != 0 {
		return nil, errors.New("unexpected fork digest in stream")
	}
	blk, err := coupledBlockDataType()
	if err != nil {
		return nil, err
	}
	err = p2p.Encoding().DecodeWithMaxLength(stream, blk)
	return blk, err
}

func coupledBlockDataType() (interfaces.CoupledBeaconBlock, error) {
	// For now only 4844 blocks are coupled
	b, err := blocks.NewSignedBeaconBlock(
		&ethpb.SignedBeaconBlockWithBlobKZGs{
			Block: &ethpb.BeaconBlockWithBlobKZGs{
				Body: &ethpb.BeaconBlockBodyWithBlobKZGs{
					ExecutionPayload: &enginev1.ExecutionPayload4844{},
				},
			},
		},
	)
	if err != nil {
		return nil, err
	}
	return blocks.BuildCoupledBeaconBlock(b, &ethpb.BlobsSidecar{})
}
