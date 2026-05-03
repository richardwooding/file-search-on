package content

import (
	"encoding/binary"
	"errors"
	"io"
)

// readMP4Info walks an MP4/M4A atom (box) tree extracting playback metadata.
//
//   - moov/mvhd          → timescale + duration (movie-level)
//   - moov/trak/mdia/minf/stbl/stsd/mp4a → channels + sample_rate
//
// Each atom is laid out as: 4-byte BE size, 4-byte ASCII type, then `size-8`
// bytes of content (which itself may contain nested atoms, depending on
// type). Size 0 means "the atom extends to EOF"; size 1 means "the next
// 8 bytes are a 64-bit extended size."
//
// References: ISO/IEC 14496-12 (Box structure), 14496-14 (MP4 file format).
func readMP4Info(r io.ReadSeeker, fileSize int64) (audioInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return audioInfo{}, err
	}
	var info audioInfo
	if err := walkAtoms(r, 0, fileSize, []string{"moov"}, func(end int64) error {
		// We're inside moov. Scan its children for mvhd and trak.
		return scanContainer(r, end, &info)
	}); err != nil {
		return info, err
	}
	return info, nil
}

// scanContainer is the workhorse: at the current reader position we expect a
// sequence of atoms up to `end`. It dispatches mvhd / trak as we encounter
// them and recurses into trak as needed.
func scanContainer(r io.ReadSeeker, end int64, info *audioInfo) error {
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		size, name, contentLen, err := readAtomHeader(r)
		if err != nil {
			return err
		}
		if size == 0 {
			// Extends to end of parent — read until end.
			contentLen = end - pos - 8
		}
		next := pos + size
		switch name {
		case "mvhd":
			if err := readMVHD(r, contentLen, info); err != nil {
				return err
			}
		case "trak":
			// Recurse: trak / mdia / minf / stbl / stsd / mp4a.
			trakEnd := next
			if err := descend(r, trakEnd, []string{"mdia", "minf", "stbl", "stsd"}, func(stsdEnd int64) error {
				return readSTSD(r, stsdEnd, info)
			}); err != nil {
				return err
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// walkAtoms scans the current container looking for the first child whose
// name matches path[0], then recurses into it with path[1:]. When path is
// empty the callback is invoked with the end offset of the matched atom.
func walkAtoms(r io.ReadSeeker, start, end int64, path []string, cb func(end int64) error) error {
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return err
	}
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		size, name, _, err := readAtomHeader(r)
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
				if err := walkAtoms(r, contentStart, next, path[1:], cb); err != nil {
					return err
				}
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// descend walks a path of single-child atoms and invokes cb at the leaf.
// Used for trak → mdia → minf → stbl → stsd, where each level has exactly
// one atom of the given name we care about.
func descend(r io.ReadSeeker, end int64, path []string, cb func(end int64) error) error {
	if len(path) == 0 {
		return cb(end)
	}
	for {
		cur, _ := r.Seek(0, io.SeekCurrent)
		if cur >= end {
			return nil
		}
		size, name, _, err := readAtomHeader(r)
		if err != nil {
			return err
		}
		next := cur + size
		if size == 0 {
			next = end
		}
		if name == path[0] {
			err := descend(r, next, path[1:], cb)
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

// readAtomHeader reads an atom header at the current position and returns
// the *full* atom size (including header), the 4-byte type as ASCII, and
// the content length (size minus header bytes).
func readAtomHeader(r io.ReadSeeker) (size int64, name string, contentLen int64, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	size32 := binary.BigEndian.Uint32(hdr[0:4])
	name = string(hdr[4:8])
	switch size32 {
	case 0:
		// Extends to EOF — caller decides.
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

func readMVHD(r io.ReadSeeker, contentLen int64, info *audioInfo) error {
	if contentLen < 16 {
		return errors.New("mvhd too short")
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	version := buf[0]
	var timescale, duration uint64
	if version == 1 {
		if len(buf) < 4+8+8+4+8 {
			return errors.New("mvhd v1 too short")
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[20:24]))
		duration = binary.BigEndian.Uint64(buf[24:32])
	} else {
		if len(buf) < 4+4+4+4+4 {
			return errors.New("mvhd v0 too short")
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[12:16]))
		duration = uint64(binary.BigEndian.Uint32(buf[16:20]))
	}
	if timescale > 0 {
		info.Duration = float64(duration) / float64(timescale)
	}
	return nil
}

// readSTSD parses the stsd box looking for the first audio sample entry.
// Layout: 1 byte version + 3 bytes flags + 4 bytes entry_count + first entry.
// Each entry starts with the standard 8-byte atom header (size + type), so
// we treat entries as atoms and dispatch on their type. Audio entries
// (mp4a, samr, etc.) have a 28-byte AudioSampleEntry preamble after the
// atom header, with channels at offset +16 and sample_rate at +24.
func readSTSD(r io.ReadSeeker, end int64, info *audioInfo) error {
	var preamble [8]byte
	if _, err := io.ReadFull(r, preamble[:]); err != nil {
		return err
	}
	// Skip the entry-count read; we always take the first entry.
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		size, name, _, err := readAtomHeader(r)
		if err != nil {
			return err
		}
		next := pos + size
		if isAudioSampleEntry(name) {
			var body [28]byte
			if _, err := io.ReadFull(r, body[:]); err != nil {
				return err
			}
			// channels at offset 16 (uint16), sample_rate fixed-point at 24:
			//   high 16 bits = integer Hz; low 16 = fraction (rarely set).
			info.Channels = int64(binary.BigEndian.Uint16(body[16:18]))
			info.SampleRate = int64(binary.BigEndian.Uint16(body[24:26]))
			return nil
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// isAudioSampleEntry returns true for atom types that are audio sample
// entries. The list is conservative — covers AAC, ALAC, plain PCM, AMR.
func isAudioSampleEntry(name string) bool {
	switch name {
	case "mp4a", "alac", "samr", "sawb", "sawp", "sevc", "sqcp", "ssmv", "twos", "sowt", "raw ":
		return true
	}
	return false
}
