package content

import (
	"encoding/binary"
	"io"
)

// MP4 / ISO base media file format box walking primitives.
//
// Each box is laid out as: 4-byte big-endian size, 4-byte ASCII type, then
// `size-8` bytes of content. Size 0 means "the box extends to EOF"; size 1
// means "the next 8 bytes are a 64-bit extended size". These helpers are
// shared by audio_mp4.go (MP4 audio metadata) and video_mp4.go (MP4 video
// metadata) — same container format, different leaf boxes.

// readBoxHeader reads a box header at the current position and returns the
// *full* box size (including header), the 4-byte type as ASCII, and the
// content length (size minus header bytes). A returned size of 0 means
// "this box extends to the end of its parent" — caller decides.
func readBoxHeader(r io.ReadSeeker) (size int64, name string, contentLen int64, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	size32 := binary.BigEndian.Uint32(hdr[0:4])
	name = string(hdr[4:8])
	switch size32 {
	case 0:
		return 0, name, 0, nil
	case 1:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		size = int64(binary.BigEndian.Uint64(ext[:]))
		contentLen = size - 16
	default:
		size = int64(size32)
		contentLen = size - 8
	}
	return
}

// walkBoxes scans a container looking for the first child whose name matches
// path[0], then recurses into it with path[1:]. When path is empty the
// callback is invoked with the end offset of the matched box.
func walkBoxes(r io.ReadSeeker, start, end int64, path []string, cb func(end int64) error) error {
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return err
	}
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		size, name, _, err := readBoxHeader(r)
		if err != nil {
			return err
		}
		next := pos + size
		if size == 0 {
			next = end
		}
		if len(path) > 0 && name == path[0] {
			contentStart, _ := r.Seek(0, io.SeekCurrent)
			if len(path) == 1 {
				if err := cb(next); err != nil {
					return err
				}
			} else {
				if err := walkBoxes(r, contentStart, next, path[1:], cb); err != nil {
					return err
				}
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// descendBoxes walks a path of single-child boxes and invokes cb at the
// leaf. Used for trak → mdia → minf → stbl → stsd, where each level has
// exactly one box of the given name we care about.
func descendBoxes(r io.ReadSeeker, end int64, path []string, cb func(end int64) error) error {
	if len(path) == 0 {
		return cb(end)
	}
	for {
		cur, _ := r.Seek(0, io.SeekCurrent)
		if cur >= end {
			return nil
		}
		size, name, _, err := readBoxHeader(r)
		if err != nil {
			return err
		}
		next := cur + size
		if size == 0 {
			next = end
		}
		if name == path[0] {
			err := descendBoxes(r, next, path[1:], cb)
			if _, e := r.Seek(next, io.SeekStart); e != nil {
				return e
			}
			return err
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}
