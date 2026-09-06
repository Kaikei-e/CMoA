package judge

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	mathrand "math/rand/v2"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// PresentationSeed returns the seed the nonce is derived from and where it
// came from: the caller's --seed, or the run id. Either way it is recorded,
// so a selection can be reproduced byte for byte from its trace.
func PresentationSeed(id trace.RunID, seed *int64) (int64, string) {
	if seed != nil {
		return *seed, "flag"
	}
	sum := sha256.Sum256([]byte(id))
	return int64(binary.BigEndian.Uint64(sum[0:8])), "run_id"
}

// Nonce is the per-selection fence label: 8 hex digits derived from the
// presentation seed.
//
// It is deliberately not from crypto/rand. The nonce has two jobs, and only
// one of them wants unpredictability. It fences the candidate blocks, which
// a candidate cannot defeat by guessing as long as the value is fresh per
// selection; and it is the one token a re-run can vary, which makes
// `--seed` a metamorphic perturbation — the same question in different
// irrelevant bytes, whose answer ought not to change. A crypto/rand nonce
// would make that perturbation unrepeatable, and a selection would not be
// reproducible from its own trace. The seed itself comes from the run id
// when the caller names none, and a run id is 8 hex from crypto/rand.
func Nonce(seed int64) string {
	//nolint:gosec // not a secret: a fence label, recorded in the trace.
	r := mathrand.New(mathrand.NewPCG(uint64(seed), ^uint64(seed)))
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(r.Uint64()>>32))
	return hex.EncodeToString(b[:])
}
