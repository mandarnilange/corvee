package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// crockford is the Crockford-Base32 alphabet used by the ULID spec
// (canonical 0-9, A-Z minus I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULIDGen produces 128-bit ULIDs with millisecond-precision timestamps
// and a cryptographically-random tail. Within the same millisecond,
// the random tail is incremented monotonically so two calls in the
// same ms still sort in call order — matching the spec's contract that
// lease_id and op_id are monotonically increasing.
//
// This is the Phase 3 replacement for the Phase 1 stub. The interface
// stays domain.IDGen — callers do not change.
type ULIDGen struct {
	mu       sync.Mutex
	lastMs   uint64
	lastRand [10]byte
}

// New returns a fresh ULIDGen.
func New() *ULIDGen { return &ULIDGen{} }

// ULID returns a 26-char Crockford-Base32 ULID. Concurrent-safe.
func (g *ULIDGen) ULID() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	// UnixMilli for any time after 1970 is positive; the ULID spec's
	// 48-bit timestamp comfortably outlives int64-positive-range so
	// the cast is safe. gosec's G115 fires on the int64→uint64 widen
	// regardless; the inline comment is the authoritative justification.
	now := time.Now().UTC().UnixMilli()
	if now < 0 {
		now = 0
	}
	ms := uint64(now) //nolint:gosec // G115: now guarded as non-negative above
	var rnd [10]byte

	if ms > g.lastMs {
		if _, err := rand.Read(rnd[:]); err != nil {
			// crypto/rand failure is essentially impossible on the
			// platforms §13 supports; fall back to a deterministic but
			// unique stamp so we never panic the entire CLI.
			binary.BigEndian.PutUint64(rnd[:8], ms)
		}
	} else {
		// Same ms (or backwards-skew) — monotonic increment of the
		// previous random tail per the ULID spec recommendation.
		ms = g.lastMs
		rnd = g.lastRand
		for i := len(rnd) - 1; i >= 0; i-- {
			rnd[i]++
			if rnd[i] != 0 {
				break
			}
		}
	}

	g.lastMs = ms
	g.lastRand = rnd
	return encodeULID(ms, rnd)
}

// encodeULID renders the 48-bit timestamp + 80-bit randomness pair as
// the 26-char Crockford-Base32 string defined by the ULID spec.
func encodeULID(ms uint64, rnd [10]byte) string {
	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	copy(b[6:], rnd[:])

	out := make([]byte, 26)
	out[0] = crockford[(b[0]&224)>>5]
	out[1] = crockford[b[0]&31]
	out[2] = crockford[(b[1]&248)>>3]
	out[3] = crockford[((b[1]&7)<<2)|((b[2]&192)>>6)]
	out[4] = crockford[(b[2]&62)>>1]
	out[5] = crockford[((b[2]&1)<<4)|((b[3]&240)>>4)]
	out[6] = crockford[((b[3]&15)<<1)|((b[4]&128)>>7)]
	out[7] = crockford[(b[4]&124)>>2]
	out[8] = crockford[((b[4]&3)<<3)|((b[5]&224)>>5)]
	out[9] = crockford[b[5]&31]
	out[10] = crockford[(b[6]&248)>>3]
	out[11] = crockford[((b[6]&7)<<2)|((b[7]&192)>>6)]
	out[12] = crockford[(b[7]&62)>>1]
	out[13] = crockford[((b[7]&1)<<4)|((b[8]&240)>>4)]
	out[14] = crockford[((b[8]&15)<<1)|((b[9]&128)>>7)]
	out[15] = crockford[(b[9]&124)>>2]
	out[16] = crockford[((b[9]&3)<<3)|((b[10]&224)>>5)]
	out[17] = crockford[b[10]&31]
	out[18] = crockford[(b[11]&248)>>3]
	out[19] = crockford[((b[11]&7)<<2)|((b[12]&192)>>6)]
	out[20] = crockford[(b[12]&62)>>1]
	out[21] = crockford[((b[12]&1)<<4)|((b[13]&240)>>4)]
	out[22] = crockford[((b[13]&15)<<1)|((b[14]&128)>>7)]
	out[23] = crockford[(b[14]&124)>>2]
	out[24] = crockford[((b[14]&3)<<3)|((b[15]&224)>>5)]
	out[25] = crockford[b[15]&31]
	return string(out)
}
