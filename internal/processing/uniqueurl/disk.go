package uniqueurl

import (
	"encoding/binary"
	"errors"
	"hash/maphash"
	"os"
)

const (
	bucketCount     = 1 << 20
	headerBytes     = 20
	writeBufferSize = 128 << 10
)

// diskSet keeps a fixed-size bucket directory in memory. Each bucket
// points to an append-only collision chain in a private temporary file.
// Hashes select candidates; membership always compares the full URL.
type diskSet struct {
	file       *os.File
	seed       maphash.Seed
	buckets    []int64
	flushed    int64
	pending    []byte
	readBuffer []byte
}

func newDiskSet() (*diskSet, error) {
	file, err := os.CreateTemp("", "reader-url-index-*")
	if err != nil {
		return nil, err
	}
	return &diskSet{
		file: file, seed: maphash.MakeSeed(), buckets: make([]int64, bucketCount),
		// Offset zero denotes the end of a chain.
		pending: append(make([]byte, 0, writeBufferSize), 0),
	}, nil
}

func (s *diskSet) add(url string) (bool, error) {
	hash := maphash.String(s.seed, url)
	bucket := hash & (bucketCount - 1)
	for offset := s.buckets[bucket]; offset != 0; {
		var header [headerBytes]byte
		if err := s.readAt(header[:], offset); err != nil {
			return false, err
		}
		length := binary.LittleEndian.Uint32(header[16:])
		if int(length) == len(url) && binary.LittleEndian.Uint64(header[8:]) == hash {
			if cap(s.readBuffer) < len(url) {
				s.readBuffer = make([]byte, len(url))
			}
			value := s.readBuffer[:len(url)]
			if err := s.readAt(value, offset+headerBytes); err != nil {
				return false, err
			}
			if string(value) == url {
				return false, nil
			}
		}
		offset = int64(binary.LittleEndian.Uint64(header[:]))
	}

	if len(s.pending)+headerBytes+len(url) > cap(s.pending) {
		if _, err := s.file.WriteAt(s.pending, s.flushed); err != nil {
			return false, err
		}
		s.flushed += int64(len(s.pending))
		s.pending = s.pending[:0]
	}
	offset := s.flushed + int64(len(s.pending))
	s.pending = binary.LittleEndian.AppendUint64(s.pending, uint64(s.buckets[bucket]))
	s.pending = binary.LittleEndian.AppendUint64(s.pending, hash)
	s.pending = binary.LittleEndian.AppendUint32(s.pending, uint32(len(url)))
	s.pending = append(s.pending, url...)
	s.buckets[bucket] = offset
	return true, nil
}

func (s *diskSet) readAt(dst []byte, offset int64) error {
	// A whole entry is written or buffered, so reads never straddle the boundary.
	if offset >= s.flushed {
		copy(dst, s.pending[offset-s.flushed:])
		return nil
	}
	_, err := s.file.ReadAt(dst, offset)
	return err
}

func (s *diskSet) close() error {
	// The index is scoped to a single run; buffered entries do not need to survive it.
	return errors.Join(s.file.Close(), os.Remove(s.file.Name()))
}
