package thumbs

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

// jpegHeader is what the segments before a JPEG's image data say about it.
type jpegHeader struct {
	orientation    int // EXIF, 1 to 8; 1 if there is none
	progressive    bool
	width, height  int
	comps          []jpegComp
	adobeTransform int // from Adobe's APP14 segment: 0 none (RGB or CMYK), 1 YCbCr, 2 YCCK; -1 without one
}

type jpegComp struct{ h, v int } // sampling factors

var errNotJPEG = errors.New("not a JPEG Go can decode")

// readJPEGHeader walks the segments up to the image data. Phones store photos the way the
// sensor saw them and say in the EXIF orientation how to turn them upright.
func readJPEGHeader(r io.Reader) (jpegHeader, error) {
	h := jpegHeader{orientation: 1, adobeTransform: -1}
	br := bufio.NewReader(r)
	var b [2]byte
	if _, err := io.ReadFull(br, b[:]); err != nil || b[0] != 0xFF || b[1] != 0xD8 {
		return h, errNotJPEG
	}
	for {
		c, err := br.ReadByte()
		if err != nil {
			return h, err
		}
		if c != 0xFF {
			return h, errNotJPEG
		}
		for c == 0xFF { // a marker may be padded with more 0xFF bytes
			if c, err = br.ReadByte(); err != nil {
				return h, err
			}
		}
		switch {
		case c == 0xDA: // start of the image data
			if h.width == 0 {
				return h, errNotJPEG
			}
			return h, nil
		case c == 0xD9:
			return h, errNotJPEG
		case c == 0x01 || (c >= 0xD0 && c <= 0xD7): // markers without a length
			continue
		}
		if _, err := io.ReadFull(br, b[:]); err != nil {
			return h, err
		}
		n := int(binary.BigEndian.Uint16(b[:])) - 2
		if n < 0 {
			return h, errNotJPEG
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(br, seg); err != nil {
			return h, err
		}
		switch {
		case c == 0xC0 || c == 0xC1 || c == 0xC2: // the frames Go decodes: baseline, extended, progressive
			if err := h.frame(seg, c == 0xC2); err != nil {
				return h, err
			}
		case c >= 0xC3 && c <= 0xCF && c != 0xC4 && c != 0xC8 && c != 0xCC: // lossless, hierarchical, arithmetic
			return h, errNotJPEG
		case c == 0xE1:
			if o := exifOrientation(seg); o != 0 {
				h.orientation = o
			}
		case c == 0xEE && len(seg) >= 12 && string(seg[:5]) == "Adobe":
			h.adobeTransform = int(seg[11])
		}
	}
}

func (h *jpegHeader) frame(seg []byte, progressive bool) error {
	if len(seg) < 6 {
		return errNotJPEG
	}
	h.progressive = progressive
	h.height = int(binary.BigEndian.Uint16(seg[1:]))
	h.width = int(binary.BigEndian.Uint16(seg[3:]))
	n := int(seg[5])
	if h.width == 0 || h.height == 0 || n < 1 || n > 4 || len(seg) < 6+3*n {
		return errNotJPEG
	}
	h.comps = h.comps[:0]
	for i := range n {
		s := seg[6+3*i+1]
		h.comps = append(h.comps, jpegComp{int(s >> 4), int(s & 0x0F)})
	}
	return nil
}

// decodeBytes estimates the memory Go's decoder needs: the planes of the decoded image, and
// for progressive files also 256 bytes per 8×8 block per component while it gathers the
// scans. That second part makes a 40 MP progressive photo need over 500 MB.
func (h jpegHeader) decodeBytes() int64 {
	hmax, vmax := 1, 1
	for _, c := range h.comps {
		hmax, vmax = max(hmax, c.h), max(vmax, c.v)
	}
	mcusX := int64((h.width + 8*hmax - 1) / (8 * hmax))
	mcusY := int64((h.height + 8*vmax - 1) / (8 * vmax))
	var n int64
	for _, c := range h.comps {
		blocks := mcusX * int64(c.h) * mcusY * int64(c.v)
		n += blocks * 64
		if h.progressive {
			n += blocks * 256
		}
	}
	// CMYK, and RGB without a colour transform, are converted into another 4 bytes per pixel.
	if len(h.comps) == 4 || (len(h.comps) == 3 && h.adobeTransform == 0) {
		n += 4 * int64(h.width) * int64(h.height)
	}
	return n
}

// jpegOrientation returns the EXIF orientation of a JPEG, or 1 if it has none or can't be read.
func jpegOrientation(r io.Reader) int {
	h, err := readJPEGHeader(r)
	if err != nil {
		return 1
	}
	return h.orientation
}

// exifOrientation reads the orientation tag from the first image directory of an APP1
// segment, or returns 0 if the segment has none.
func exifOrientation(seg []byte) int {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0
	}
	t := seg[6:] // the TIFF structure; offsets count from here
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(t[2:]) != 42 {
		return 0
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	entries := int(bo.Uint16(t[ifd:]))
	for i := range entries {
		e := ifd + 2 + i*12
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) != 0x0112 {
			continue
		}
		// A SHORT: the value sits in the first two bytes of the value field.
		if bo.Uint16(t[e+2:]) != 3 {
			return 0
		}
		if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
			return o
		}
		return 0
	}
	return 0
}
