package boot

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"sort"
)

// String returns "sha256:" and the lowercase hex of the digest.
func (d Digest) String() string { return "sha256:" + hex.EncodeToString(d[:]) }

// digestMagic starts every encoding and names its version.
const digestMagic = "aiops/composition/v1\n"

// EncodeDigest computes the digest of a composition: SHA-256 over the
// canonical encoding below, so that anyone can reimplement it. The inputs are
// the composed tree, the identity fields of the module table and the lock
// digests (08-profiles-roadmap-adrs.md:184). Provenance, source lines,
// references (derived from the tree), the plugin handle and the provides,
// needs and volatile lists of a module entry are not part of it.
//
// Primitives:
//
//	uvarint   unsigned LEB128, as encoding/binary.AppendUvarint
//	str       uvarint(byte length) then the bytes (no normalisation)
//	bool      one byte, 0x00 or 0x01
//	strs      uvarint(count) then each str, sorted by byte order
//	value     one tag byte, then:
//	            'n' null: nothing
//	            't' or 'f': a bool, nothing more
//	            'i' int: 8 bytes, big-endian two's complement
//	            'd' float: 8 bytes, big-endian IEEE 754 bits, -0 as +0
//	            's' string: str
//	            'r' reference: str(scheme) str(name)
//	            'l' list: uvarint(count) then each value, in order
//	            'm' map: uvarint(count) then str(key) value, sorted by key
//
// So the integer 1 and the float 1.0 differ, while 1e3 and 1000.0 are the
// same float, and a string is never confused with a reference.
//
// The stream is the magic text "aiops/composition/v1\n", then
//
//	rows:    uvarint(count), then for each row sorted by id:
//	         str(id) str(plugin) str(parent) value(config) bool(enabled)
//	         bool(required) strs(roles) strs(inject) value(intercept as a map)
//	         strs(isolate) bool(group)
//	modules: uvarint(count), then for each entry sorted by plugin id:
//	         str(id) str(module) str(version) str(sum)
//	locks:   uvarint(count), then for each lock sorted by name:
//	         str(name) str(digest)
//
// Rows are sorted because row order carries no load semantics
// (08-profiles-roadmap-adrs.md:156). The digest is SHA-256 of the whole stream.
func EncodeDigest(rows []Row, modules ModuleTable, locks []LockDigest) Digest {
	b := []byte(digestMagic)
	sorted := append([]Row(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	b = binary.AppendUvarint(b, uint64(len(sorted)))
	for _, r := range sorted {
		b = putStr(b, r.ID)
		b = putStr(b, r.Plugin)
		b = putStr(b, r.Parent)
		b = putValue(b, r.Config)
		b = putBool(b, r.Enabled)
		b = putBool(b, r.Required)
		roles := make([]string, len(r.Roles))
		for i, x := range r.Roles {
			roles[i] = string(x)
		}
		b = putStrs(b, roles)
		b = putStrs(b, r.Inject)
		b = putValue(b, Value{Kind: KindMap, Map: r.Intercept})
		b = putStrs(b, r.Isolate)
		b = putBool(b, r.Group)
	}
	ids := make([]string, 0, len(modules))
	for id := range modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b = binary.AppendUvarint(b, uint64(len(ids)))
	for _, id := range ids {
		m := modules[id]
		b = putStr(b, id)
		b = putStr(b, m.Module)
		b = putStr(b, m.Version)
		b = putStr(b, m.Sum)
	}
	ls := append([]LockDigest(nil), locks...)
	sort.Slice(ls, func(i, j int) bool { return ls[i].Name < ls[j].Name })
	b = binary.AppendUvarint(b, uint64(len(ls)))
	for _, l := range ls {
		b = putStr(b, l.Name)
		b = putStr(b, l.Digest)
	}
	return sha256.Sum256(b)
}

func putStr(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func putBool(b []byte, v bool) []byte {
	if v {
		return append(b, 1)
	}
	return append(b, 0)
}

func putStrs(b []byte, s []string) []byte {
	s = append([]string(nil), s...)
	sort.Strings(s)
	b = binary.AppendUvarint(b, uint64(len(s)))
	for _, x := range s {
		b = putStr(b, x)
	}
	return b
}

func putValue(b []byte, v Value) []byte {
	switch v.Kind {
	case KindBool:
		if v.Bool {
			return append(b, 't')
		}
		return append(b, 'f')
	case KindInt:
		return binary.BigEndian.AppendUint64(append(b, 'i'), uint64(v.Int))
	case KindFloat:
		f := v.Float
		if f == 0 {
			f = 0 // folds negative zero
		}
		return binary.BigEndian.AppendUint64(append(b, 'd'), math.Float64bits(f))
	case KindString:
		return putStr(append(b, 's'), v.Str)
	case KindRef:
		return putStr(putStr(append(b, 'r'), string(v.Ref.Scheme)), v.Ref.Name)
	case KindList:
		b = binary.AppendUvarint(append(b, 'l'), uint64(len(v.List)))
		for _, x := range v.List {
			b = putValue(b, x)
		}
		return b
	case KindMap:
		fs := append([]Field(nil), v.Map...)
		sort.Slice(fs, func(i, j int) bool { return fs[i].Key < fs[j].Key })
		b = binary.AppendUvarint(append(b, 'm'), uint64(len(fs)))
		for _, f := range fs {
			b = putValue(putStr(b, f.Key), f.Value)
		}
		return b
	}
	return append(b, 'n')
}
