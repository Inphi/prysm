package detect

import (
	"fmt"

	state_native "github.com/prysmaticlabs/prysm/v3/beacon-chain/state/state-native"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v3/consensus-types/interfaces"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	"github.com/prysmaticlabs/prysm/v3/network/forks"

	"github.com/pkg/errors"
	ssz "github.com/prysmaticlabs/fastssz"
	"github.com/prysmaticlabs/prysm/v3/beacon-chain/state"
	fieldparams "github.com/prysmaticlabs/prysm/v3/config/fieldparams"
	"github.com/prysmaticlabs/prysm/v3/config/params"
	types "github.com/prysmaticlabs/prysm/v3/consensus-types/primitives"
	ethpb "github.com/prysmaticlabs/prysm/v3/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/v3/runtime/version"
	"github.com/prysmaticlabs/prysm/v3/time/slots"
)

// VersionedUnmarshaler represents the intersection of Configuration (eg mainnet, testnet) and Fork (eg phase0, altair).
// Using a detected VersionedUnmarshaler, a BeaconState or SignedBeaconBlock can be correctly unmarshaled without the need to
// hard code a concrete type in paths where only the marshaled bytes, or marshaled bytes and a version, are available.
type VersionedUnmarshaler struct {
	Config *params.BeaconChainConfig
	// Fork aligns with the fork names in config/params/values.go
	Fork int
	// Version corresponds to the Version type defined in the beacon-chain spec, aka a "fork version number":
	// https://github.com/ethereum/consensus-specs/blob/dev/specs/phase0/beacon-chain.md#custom-types
	Version [fieldparams.VersionLength]byte
}

var beaconStateCurrentVersion = fieldSpec{
	// 52 = 8 (genesis_time) + 32 (genesis_validators_root) + 8 (slot) + 4 (previous_version)
	offset: 52,
	t:      typeBytes4,
}

// FromState exploits the fixed-size lower-order bytes in a BeaconState as a heuristic to obtain the value of the
// state.version field without first unmarshaling the BeaconState. The Version is then internally used to lookup
// the correct ConfigVersion.
func FromState(marshaled []byte) (*VersionedUnmarshaler, error) {
	cv, err := beaconStateCurrentVersion.bytes4(marshaled)
	if err != nil {
		return nil, err
	}
	return FromForkVersion(cv)
}

var ErrForkNotFound = errors.New("version found in fork schedule but can't be matched to a named fork")

// FromForkVersion uses a lookup table to resolve a Version (from a beacon node api for instance, or obtained by peeking at
// the bytes of a marshaled BeaconState) to a VersionedUnmarshaler.
func FromForkVersion(cv [fieldparams.VersionLength]byte) (*VersionedUnmarshaler, error) {
	cfg, err := params.ByVersion(cv)
	if err != nil {
		return nil, err
	}
	var fork int
	switch cv {
	case bytesutil.ToBytes4(cfg.GenesisForkVersion):
		fork = version.Phase0
	case bytesutil.ToBytes4(cfg.AltairForkVersion):
		fork = version.Altair
	case bytesutil.ToBytes4(cfg.BellatrixForkVersion):
		fork = version.Bellatrix
	case bytesutil.ToBytes4(cfg.Eip4844ForkVersion):
		fork = version.EIP4844
	default:
		return nil, errors.Wrapf(ErrForkNotFound, "version=%#x", cv)
	}
	return &VersionedUnmarshaler{
		Config:  cfg,
		Fork:    fork,
		Version: cv,
	}, nil
}

// UnmarshalBeaconState uses internal knowledge in the VersionedUnmarshaler to pick the right concrete BeaconState type,
// then Unmarshal()s the type and returns an instance of state.BeaconState if successful.
func (cf *VersionedUnmarshaler) UnmarshalBeaconState(marshaled []byte) (s state.BeaconState, err error) {
	forkName := version.String(cf.Fork)
	switch fork := cf.Fork; fork {
	case version.Phase0:
		st := &ethpb.BeaconState{}
		err = st.UnmarshalSSZ(marshaled)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal state, detected fork=%s", forkName)
		}
		s, err = state_native.InitializeFromProtoUnsafePhase0(st)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to init state trie from state, detected fork=%s", forkName)
		}
	case version.Altair:
		st := &ethpb.BeaconStateAltair{}
		err = st.UnmarshalSSZ(marshaled)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal state, detected fork=%s", forkName)
		}
		s, err = state_native.InitializeFromProtoUnsafeAltair(st)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to init state trie from state, detected fork=%s", forkName)
		}
	case version.Bellatrix:
		st := &ethpb.BeaconStateBellatrix{}
		err = st.UnmarshalSSZ(marshaled)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal state, detected fork=%s", forkName)
		}
		s, err = state_native.InitializeFromProtoUnsafeBellatrix(st)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to init state trie from state, detected fork=%s", forkName)
		}
	case version.EIP4844:
		st := &ethpb.BeaconState4844{}
		err = st.UnmarshalSSZ(marshaled)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal state, detected fork=%s", forkName)
		}
		s, err = state_native.InitializeFromProtoUnsafe4844(st)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to init state trie from state, detected fork=%s", forkName)
		}
	default:
		return nil, fmt.Errorf("unable to initialize BeaconState for fork version=%s", forkName)
	}

	return s, nil
}

var beaconBlockSlot = fieldSpec{
	// ssz variable length offset (not to be confused with the fieldSpec offest) is a uint32
	// variable length. Offsets come before fixed length data, so that's 4 bytes at the beginning
	// then signature is 96 bytes, 4+96 = 100
	offset: 100,
	t:      typeUint64,
}

func slotFromBlock(marshaled []byte) (types.Slot, error) {
	slot, err := beaconBlockSlot.uint64(marshaled)
	if err != nil {
		return 0, err
	}
	return types.Slot(slot), nil
}

var errBlockForkMismatch = errors.New("fork or config detected in unmarshaler is different than block")

// UnmarshalBeaconBlock uses internal knowledge in the VersionedUnmarshaler to pick the right concrete SignedBeaconBlock type,
// then Unmarshal()s the type and returns an instance of block.SignedBeaconBlock if successful.
func (cf *VersionedUnmarshaler) UnmarshalBeaconBlock(marshaled []byte) (interfaces.SignedBeaconBlock, error) {
	slot, err := slotFromBlock(marshaled)
	if err != nil {
		return nil, err
	}
	if err := cf.validateVersion(slot); err != nil {
		return nil, err
	}

	var blk ssz.Unmarshaler
	switch cf.Fork {
	case version.Phase0:
		blk = &ethpb.SignedBeaconBlock{}
	case version.Altair:
		blk = &ethpb.SignedBeaconBlockAltair{}
	case version.Bellatrix:
		blk = &ethpb.SignedBeaconBlockBellatrix{}
	case version.EIP4844:
		blk = &ethpb.SignedBeaconBlockWithBlobKZGs{}
	default:
		forkName := version.String(cf.Fork)
		return nil, fmt.Errorf("unable to initialize BeaconBlock for fork version=%s at slot=%d", forkName, slot)
	}
	err = blk.UnmarshalSSZ(marshaled)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal SignedBeaconBlock in UnmarshalSSZ")
	}
	return blocks.NewSignedBeaconBlock(blk)
}

// UnmarshalBlindedBeaconBlock uses internal knowledge in the VersionedUnmarshaler to pick the right concrete blinded SignedBeaconBlock type,
// then Unmarshal()s the type and returns an instance of block.SignedBeaconBlock if successful.
// For Phase0 and Altair it works exactly line UnmarshalBeaconBlock.
func (cf *VersionedUnmarshaler) UnmarshalBlindedBeaconBlock(marshaled []byte) (interfaces.SignedBeaconBlock, error) {
	slot, err := slotFromBlock(marshaled)
	if err != nil {
		return nil, err
	}
	if err := cf.validateVersion(slot); err != nil {
		return nil, err
	}

	var blk ssz.Unmarshaler
	switch cf.Fork {
	case version.Phase0:
		blk = &ethpb.SignedBeaconBlock{}
	case version.Altair:
		blk = &ethpb.SignedBeaconBlockAltair{}
	case version.Bellatrix:
		blk = &ethpb.SignedBlindedBeaconBlockBellatrix{}
	case version.EIP4844:
		blk = &ethpb.SignedBeaconBlockWithBlobKZGs{}
	default:
		forkName := version.String(cf.Fork)
		return nil, fmt.Errorf("unable to initialize BeaconBlock for fork version=%s at slot=%d", forkName, slot)
	}
	err = blk.UnmarshalSSZ(marshaled)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal SignedBeaconBlock in UnmarshalSSZ")
	}
	return blocks.NewSignedBeaconBlock(blk)
}

// Heuristic to make sure block is from the same version as the VersionedUnmarshaler.
// Look up the version for the epoch that the block is from, then ensure that it matches the Version in the
// VersionedUnmarshaler.
func (cf *VersionedUnmarshaler) validateVersion(slot types.Slot) error {
	epoch := slots.ToEpoch(slot)
	fs := forks.NewOrderedSchedule(cf.Config)
	ver, err := fs.VersionForEpoch(epoch)
	if err != nil {
		return err
	}
	if ver != cf.Version {
		return errors.Wrapf(errBlockForkMismatch, "slot=%d, epoch=%d, version=%#x", slot, epoch, ver)
	}
	return nil
}
