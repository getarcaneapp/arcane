package utils

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"time"
)

// Fingerprint accumulates an order-sensitive 64-bit hash of arbitrary field
// values. It exists for change detection: a poller that must decide "did
// anything change since the last tick?" can hash the fields it cares about
// instead of marshalling the whole payload to JSON and retaining the bytes just
// to compare them on the next tick.
//
// Every method returns the receiver so a struct's fields read as one chain.
// Opt* variants tag presence, keeping nil distinct from the zero value so
// clearing a field is observed as a change.
//
// It is a change detector, not a checksum. FNV-1a is not collision resistant and
// must never back a security or correctness decision.
type Fingerprint struct {
	h hash.Hash64
}

func NewFingerprint() *Fingerprint {
	return &Fingerprint{h: fnv.New64a()}
}

// String hashes s followed by a separator, so adjacent fields cannot be
// re-split ("ab"+"c" must not match "a"+"bc").
func (f *Fingerprint) String(s string) *Fingerprint {
	_, _ = f.h.Write([]byte(s))
	_, _ = f.h.Write([]byte{0})
	return f
}

func (f *Fingerprint) OptString(s *string) *Fingerprint {
	if s == nil {
		return f.Bool(false)
	}
	return f.Bool(true).String(*s)
}

// Strings hashes a string slice including its length, so resizing or reordering
// changes the result.
func (f *Fingerprint) Strings(items []string) *Fingerprint {
	f.Int(int64(len(items)))
	for _, item := range items {
		f.String(item)
	}
	return f
}

func (f *Fingerprint) Bool(b bool) *Fingerprint {
	if b {
		_, _ = f.h.Write([]byte{1})
		return f
	}
	_, _ = f.h.Write([]byte{0})
	return f
}

func (f *Fingerprint) OptBool(b *bool) *Fingerprint {
	if b == nil {
		return f.Bool(false)
	}
	return f.Bool(true).Bool(*b)
}

func (f *Fingerprint) Int(v int64) *Fingerprint {
	// Varint keeps the encoding signed end to end; a fixed-width write would
	// need an int64→uint64 reinterpretation.
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], v)
	_, _ = f.h.Write(buf[:n])
	return f
}

func (f *Fingerprint) OptInt(v *int) *Fingerprint {
	if v == nil {
		return f.Bool(false)
	}
	return f.Bool(true).Int(int64(*v))
}

func (f *Fingerprint) Time(t time.Time) *Fingerprint {
	return f.Int(t.UnixNano())
}

func (f *Fingerprint) OptTime(t *time.Time) *Fingerprint {
	if t == nil {
		return f.Bool(false)
	}
	return f.Bool(true).Time(*t)
}

// Present tags whether an optional nested value follows. Hash its fields only
// when it reports true.
func (f *Fingerprint) Present(ok bool) *Fingerprint {
	return f.Bool(ok)
}

func (f *Fingerprint) Sum() uint64 {
	return f.h.Sum64()
}

// Slice hashes a slice of items with a per-item writer, including the slice
// length so resizing or reordering changes the result. The writer receives a
// pointer so large structs are not copied per item.
func (f *Fingerprint) Slice[T any](items []T, write func(*Fingerprint, *T)) *Fingerprint {
	f.Int(int64(len(items)))
	for i := range items {
		write(f, &items[i])
	}
	return f
}
