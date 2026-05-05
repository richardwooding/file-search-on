package content

import (
	"encoding/binary"
	"errors"
	"io"
)

// readAVIInfo parses the RIFF chunks of an AVI file extracting playback +
// video metadata.
//
// AVI layout: "RIFF" + 4-byte LE size + "AVI " + sequence of LIST chunks.
// We care about LIST hdrl, which contains:
//
//   - avih (main AVI header, 56 bytes): microSecPerFrame, totalFrames,
//     width, height
//   - LIST strl (one per stream), each containing strh (Stream Header)
//     with fccType ('vids' / 'auds') and fccHandler (codec FOURCC)
//
// References: AVI RIFF File Reference (Microsoft).
func readAVIInfo(r io.ReadSeeker, fileSize int64) (videoInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return videoInfo{}, err
	}

	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return videoInfo{}, err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "AVI " {
		return videoInfo{}, errors.New("not a RIFF AVI file")
	}

	var info videoInfo
	end := fileSize
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos+8 > end {
			return info, nil
		}
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return info, nil
		}
		size := int64(binary.LittleEndian.Uint32(ch[4:8]))
		// Chunks are word-aligned; pad odd sizes by 1.
		nextPos := pos + 8 + size
		if size%2 == 1 {
			nextPos++
		}
		switch string(ch[0:4]) {
		case "LIST":
			var listType [4]byte
			if _, err := io.ReadFull(r, listType[:]); err != nil {
				return info, nil
			}
			if string(listType[:]) == "hdrl" {
				if err := readAVIHDRL(r, pos+8+size, &info); err != nil {
					return info, err
				}
			}
		}
		if _, err := r.Seek(nextPos, io.SeekStart); err != nil {
			return info, nil
		}
	}
}

// readAVIHDRL walks an hdrl LIST looking for the avih main header and
// strl LISTs. The reader is positioned just after the "hdrl" type tag.
func readAVIHDRL(r io.ReadSeeker, end int64, info *videoInfo) error {
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos+8 > end {
			return nil
		}
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return nil
		}
		size := int64(binary.LittleEndian.Uint32(ch[4:8]))
		nextPos := pos + 8 + size
		if size%2 == 1 {
			nextPos++
		}
		switch string(ch[0:4]) {
		case "avih":
			readAVIH(r, size, info)
		case "LIST":
			var listType [4]byte
			if _, err := io.ReadFull(r, listType[:]); err != nil {
				return nil
			}
			if string(listType[:]) == "strl" {
				if err := readAVISTRL(r, pos+8+size, info); err != nil {
					return err
				}
			}
		}
		if _, err := r.Seek(nextPos, io.SeekStart); err != nil {
			return nil
		}
	}
}

// readAVIH parses the main AVI header. Layout (56 bytes):
//
//	uint32 microSecPerFrame
//	uint32 maxBytesPerSec
//	uint32 paddingGranularity
//	uint32 flags
//	uint32 totalFrames
//	uint32 initialFrames
//	uint32 streams
//	uint32 suggestedBufferSize
//	uint32 width
//	uint32 height
//	uint32 reserved[4]
func readAVIH(r io.ReadSeeker, size int64, info *videoInfo) {
	if size < 40 {
		return
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return
	}
	microSecPerFrame := binary.LittleEndian.Uint32(buf[0:4])
	maxBytesPerSec := binary.LittleEndian.Uint32(buf[4:8])
	totalFrames := binary.LittleEndian.Uint32(buf[16:20])
	width := binary.LittleEndian.Uint32(buf[32:36])
	height := binary.LittleEndian.Uint32(buf[36:40])

	if microSecPerFrame > 0 {
		info.FrameRate = 1e6 / float64(microSecPerFrame)
		info.Duration = float64(totalFrames) * float64(microSecPerFrame) / 1e6
	}
	info.Width = int64(width)
	info.Height = int64(height)
	// maxBytesPerSec is the AVI header's overall max-bandwidth field; convert
	// to kbps for nominal_bitrate. It's the whole file's playback envelope
	// (video + audio combined), not strictly a video-track value, but it's
	// the closest codec-stored bitrate AVI carries.
	if maxBytesPerSec > 0 {
		info.NominalBitrate = int64(maxBytesPerSec) * 8 / 1000
	}
}

// readAVISTRL walks an strl LIST looking for the strh stream header and
// strf format header. The reader is positioned just after the "strl" type
// tag. fccType from strh is remembered so the immediately-following strf
// is parsed with the right struct (BITMAPINFOHEADER for vids,
// WAVEFORMATEX for auds).
func readAVISTRL(r io.ReadSeeker, end int64, info *videoInfo) error {
	var fccType string
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos+8 > end {
			return nil
		}
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return nil
		}
		size := int64(binary.LittleEndian.Uint32(ch[4:8]))
		nextPos := pos + 8 + size
		if size%2 == 1 {
			nextPos++
		}
		switch string(ch[0:4]) {
		case "strh":
			fccType = readSTRH(r, size, info)
		case "strf":
			if fccType == "auds" {
				readSTRFAuds(r, size, info)
			}
		}
		if _, err := r.Seek(nextPos, io.SeekStart); err != nil {
			return nil
		}
	}
}

// readSTRH reads the AVIStreamHeader (56 bytes), populating video_codec
// or audio_codec depending on fccType. Returns the fccType so the caller
// can dispatch the following strf chunk.
func readSTRH(r io.ReadSeeker, size int64, info *videoInfo) string {
	if size < 8 {
		return ""
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return ""
	}
	fccType := string(buf[0:4])
	fccHandler := string(buf[4:8])
	switch fccType {
	case "vids":
		info.VideoCodec = aviCodecName(fccHandler)
	case "auds":
		// fccHandler is usually "0\x00\x00\x00" (twoCC for audio); the
		// real codec id is in the WAVEFORMATEX in strf. Surface the
		// FOURCC if present, otherwise leave blank.
		if codec := aviCodecName(fccHandler); codec != "" {
			info.AudioCodec = codec
		}
	}
	return fccType
}

// readSTRFAuds reads a WAVEFORMATEX struct from an auds stream's strf
// chunk and populates info.AudioSampleRate / info.AudioChannels.
//
// WAVEFORMATEX layout (16+ bytes, little-endian):
//
//	WORD  wFormatTag       // 2 bytes
//	WORD  nChannels        // 2 bytes
//	DWORD nSamplesPerSec   // 4 bytes
//	DWORD nAvgBytesPerSec  // 4 bytes
//	WORD  nBlockAlign      // 2 bytes
//	WORD  wBitsPerSample   // 2 bytes
//	WORD  cbSize           // 2 bytes
func readSTRFAuds(r io.ReadSeeker, size int64, info *videoInfo) {
	if size < 8 {
		return
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return
	}
	if info.AudioChannels == 0 {
		info.AudioChannels = int64(binary.LittleEndian.Uint16(buf[2:4]))
	}
	if info.AudioSampleRate == 0 {
		info.AudioSampleRate = int64(binary.LittleEndian.Uint32(buf[4:8]))
	}
}

// aviCodecName maps an AVI FOURCC to a friendly codec name. Only common
// codecs are mapped; everything else is returned as-is (lowercased) so
// users can still filter on rarer codecs by their FOURCC.
func aviCodecName(fourcc string) string {
	// Trim trailing nulls/spaces.
	for len(fourcc) > 0 && (fourcc[len(fourcc)-1] == 0 || fourcc[len(fourcc)-1] == ' ') {
		fourcc = fourcc[:len(fourcc)-1]
	}
	if fourcc == "" {
		return ""
	}
	switch fourcc {
	case "H264", "AVC1", "X264", "h264", "avc1", "x264":
		return "h264"
	case "HEVC", "H265", "X265", "hev1", "hvc1":
		return "h265"
	case "DIVX", "divx":
		return "divx"
	case "XVID", "xvid":
		return "xvid"
	case "MP4V", "mp4v":
		return "mpeg4"
	case "MJPG", "mjpg":
		return "mjpeg"
	case "VP80", "vp80":
		return "vp8"
	case "VP90", "vp90":
		return "vp9"
	case "AV01", "av01":
		return "av1"
	}
	return fourcc
}
