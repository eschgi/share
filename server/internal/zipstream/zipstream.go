// Package zipstream writes a ZIP archive of stored files whose bytes depend only on the
// entries' names, sizes and times, so its size and any byte range are known before a file
// is read. A download of many files gets an exact Content-Length and a strong ETag that
// way, and a broken one resumes with Range where it stopped.
//
// The files are stored as they are; photos and videos don't get smaller in a ZIP anyway.
// Each local header carries the file's CRC-32 and sizes, and no data descriptor follows
// the data: Java's ZipInputStream, which Android apps unzip with, can't read stored entries
// that have one. Whatever doesn't fit in 32 bits goes into Zip64 fields (APPNOTE.TXT 6.3).
package zipstream

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Entry is a file in the archive.
type Entry struct {
	Name     string // slash-separated path inside the archive, e.g. "2026-09-27/IMG_0001.jpg"
	Size     int64
	Modified time.Time
}

// File is an entry's data, e.g. an *os.File.
type File interface {
	io.ReaderAt
	io.Closer
}

// Source gives the archive its files' contents and checksums. i is the entry's index in the
// list given to New.
type Source interface {
	CRC32(ctx context.Context, i int) (uint32, error) // entry i's CRC-32 (IEEE); may block while it's computed
	Open(i int) (File, error)                         // entry i's data
}

const (
	// A 16- or 32-bit field holding its largest value says: look in the Zip64 records.
	uint16max = 1<<16 - 1
	uint32max = 1<<32 - 1

	localSig     = 0x04034b50
	centralSig   = 0x02014b50
	end64Sig     = 0x06064b50
	locator64Sig = 0x07064b50
	endSig       = 0x06054b50

	version20 = 20 // 2.0, which every unzip tool reads
	version45 = 45 // 4.5, for Zip64

	// Bit 11 says the name is UTF-8. Bit 3, for a data descriptor, stays clear.
	flagUTF8 = 0x0800

	zip64Tag     = 0x0001
	timeTag      = 0x5455 // Info-ZIP's extended timestamp: seconds since 1970, in UTC
	timeExtraLen = 9      // its tag, size, flags and time

	// maxSize keeps the offsets far from overflowing; no drive comes close to it.
	maxSize = 1 << 62
)

var le = binary.LittleEndian

// Archive is the layout of an archive. It doesn't change after New, so any number of
// Readers may read it at once.
type Archive struct {
	entries []entry
	dir     int64 // where the central directory starts
	dirLen  int64
	end     []byte // the end records, which need no checksums
	size    int64
}

type entry struct {
	name       string
	size       int64
	offset     int64  // of the local header
	time, date uint16 // MS-DOS
	mtime      uint32 // for the extended timestamp
}

// New lays out an archive. It checks the names: not empty, valid UTF-8, no NUL or
// backslash, no leading "/", no empty, "." or ".." elements, at most 65535 bytes, and no
// name twice. The MS-DOS times in the headers are the wall clock in loc, which is what
// tools without the extended timestamp show.
func New(entries []Entry, loc *time.Location) (*Archive, error) {
	a := &Archive{entries: make([]entry, len(entries))}
	seen := make(map[string]bool, len(entries))
	var off int64
	for i, e := range entries {
		if err := checkName(e.Name); err != nil {
			return nil, fmt.Errorf("zipstream: entry %d: %w", i, err)
		}
		if seen[e.Name] {
			return nil, fmt.Errorf("zipstream: %q is in the list twice", e.Name)
		}
		seen[e.Name] = true
		x := entry{name: e.Name, size: e.Size, offset: off, mtime: unixTime(e.Modified)}
		x.time, x.date = msDOSTime(e.Modified.In(loc))
		data := off + x.localLen()
		if e.Size < 0 || e.Size > maxSize-data {
			return nil, fmt.Errorf("zipstream: %q can't be %d bytes long", e.Name, e.Size)
		}
		a.entries[i] = x
		off = data + e.Size
	}
	a.dir = off
	for i := range a.entries {
		a.dirLen += a.entries[i].centralLen()
	}
	a.end = a.appendEnd(nil)
	a.size = a.dir + a.dirLen + int64(len(a.end))
	return a, nil
}

// Size is the exact length of the archive in bytes.
func (a *Archive) Size() int64 { return a.size }

// checkName refuses names a header can't hold or unzip tools would misread: a leading "/"
// or a ".." leads out of the folder being unpacked into, a trailing "/" makes a folder, "."
// and empty elements let two names land on one file, Windows tools take a backslash for a
// separator, and C code ends a name at NUL.
func checkName(name string) error {
	switch {
	case name == "":
		return errors.New("the name is empty")
	case len(name) > uint16max:
		return fmt.Errorf("a name of %d bytes is too long", len(name))
	case !utf8.ValidString(name):
		return fmt.Errorf("%q is not UTF-8", name)
	case strings.ContainsAny(name, "\x00\\"):
		return fmt.Errorf("%q contains NUL or a backslash", name)
	case strings.HasPrefix(name, "/"):
		return fmt.Errorf("%q starts with a slash", name)
	}
	for el := range strings.SplitSeq(name, "/") {
		if el == "" || el == "." || el == ".." {
			return fmt.Errorf("%q has an empty, \".\" or \"..\" element", name)
		}
	}
	return nil
}

// msDOSTime encodes a wall clock time the MS-DOS way, which covers 1980 to 2107 in steps
// of two seconds. Earlier and later times get the first or last one there is.
func msDOSTime(t time.Time) (tm, date uint16) {
	switch {
	case t.Year() < 1980:
		t = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	case t.Year() > 2107:
		t = time.Date(2107, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	return uint16(t.Hour()<<11 | t.Minute()<<5 | t.Second()/2),
		uint16((t.Year()-1980)<<9 | int(t.Month())<<5 | t.Day())
}

// unixTime is t for the extended timestamp, which has 32 bits for the seconds since 1970.
// Java and Android read them as a signed number, so times stop at January 2038 rather than
// 2106; earlier ones get 1970.
func unixTime(t time.Time) uint32 { return uint32(min(max(t.Unix(), 0), math.MaxInt32)) }

// zip64 reports whether the sizes go into a Zip64 extra. A size of 0xFFFFFFFF does too,
// since that value in a size field means "look in the extra".
func (e *entry) zip64() bool { return e.size >= uint32max }

// escaped counts the fields of the central header that go into its Zip64 extra.
func (e *entry) escaped() int {
	k := 0
	if e.zip64() {
		k += 2 // both sizes
	}
	if e.offset >= uint32max {
		k++
	}
	return k
}

func (e *entry) localLen() int64 {
	n := int64(30 + len(e.name) + timeExtraLen)
	if e.zip64() {
		n += 20
	}
	return n
}

func (e *entry) centralLen() int64 {
	n := int64(46 + len(e.name) + timeExtraLen)
	if k := e.escaped(); k > 0 {
		n += int64(4 + 8*k)
	}
	return n
}

func (e *entry) appendLocal(b []byte, crc uint32) []byte {
	version, size, extra := uint16(version20), uint32(e.size), uint16(timeExtraLen)
	if e.zip64() {
		version, size, extra = version45, uint32max, extra+20
	}
	b = le.AppendUint32(b, localSig)
	b = le.AppendUint16(b, version)
	b = le.AppendUint16(b, flagUTF8)
	b = le.AppendUint16(b, 0) // stored
	b = le.AppendUint16(b, e.time)
	b = le.AppendUint16(b, e.date)
	b = le.AppendUint32(b, crc)
	b = le.AppendUint32(b, size) // compressed
	b = le.AppendUint32(b, size) // uncompressed
	b = le.AppendUint16(b, uint16(len(e.name)))
	b = le.AppendUint16(b, extra)
	b = append(b, e.name...)
	if e.zip64() {
		// A local Zip64 extra has both sizes, always.
		b = le.AppendUint16(b, zip64Tag)
		b = le.AppendUint16(b, 16)
		b = le.AppendUint64(b, uint64(e.size))
		b = le.AppendUint64(b, uint64(e.size))
	}
	return e.appendTime(b)
}

func (e *entry) appendCentral(b []byte, crc uint32) []byte {
	k := e.escaped()
	version, extra := uint16(version20), uint16(timeExtraLen)
	if k > 0 {
		version, extra = version45, extra+uint16(4+8*k)
	}
	size, offset := uint32(e.size), uint32(e.offset)
	if e.zip64() {
		size = uint32max
	}
	if e.offset >= uint32max {
		offset = uint32max
	}
	b = le.AppendUint32(b, centralSig)
	b = le.AppendUint16(b, version) // made by: on MS-DOS (high byte 0), so unzip applies the umask
	b = le.AppendUint16(b, version) // needed
	b = le.AppendUint16(b, flagUTF8)
	b = le.AppendUint16(b, 0) // stored
	b = le.AppendUint16(b, e.time)
	b = le.AppendUint16(b, e.date)
	b = le.AppendUint32(b, crc)
	b = le.AppendUint32(b, size) // compressed
	b = le.AppendUint32(b, size) // uncompressed
	b = le.AppendUint16(b, uint16(len(e.name)))
	b = le.AppendUint16(b, extra)
	b = le.AppendUint16(b, 0) // comment length
	b = le.AppendUint16(b, 0) // disk
	b = le.AppendUint16(b, 0) // internal attributes
	b = le.AppendUint32(b, 0) // external attributes
	b = le.AppendUint32(b, offset)
	b = append(b, e.name...)
	if k > 0 {
		// Only the fields that didn't fit, in this order.
		b = le.AppendUint16(b, zip64Tag)
		b = le.AppendUint16(b, uint16(8*k))
		if e.zip64() {
			b = le.AppendUint64(b, uint64(e.size))
			b = le.AppendUint64(b, uint64(e.size))
		}
		if e.offset >= uint32max {
			b = le.AppendUint64(b, uint64(e.offset))
		}
	}
	return e.appendTime(b)
}

// appendTime appends the extended timestamp, which unzip tools prefer to the MS-DOS time:
// it is in UTC and exact to the second.
func (e *entry) appendTime(b []byte) []byte {
	b = le.AppendUint16(b, timeTag)
	b = le.AppendUint16(b, 5)
	b = append(b, 1) // just the modification time
	return le.AppendUint32(b, e.mtime)
}

// appendEnd appends the end of central directory record, after the Zip64 ones when the
// number of entries, or the size or start of the directory, doesn't fit. An entry with
// Zip64 fields puts the directory past 4 GiB, so the Zip64 records are there whenever an
// entry has such fields, as some readers expect.
func (a *Archive) appendEnd(b []byte) []byte {
	n, dirLen, dir := uint64(len(a.entries)), uint64(a.dirLen), uint64(a.dir)
	n16, dirLen32, dir32 := uint16(n), uint32(dirLen), uint32(dir)
	if n >= uint16max || dirLen >= uint32max || dir >= uint32max {
		b = le.AppendUint32(b, end64Sig)
		b = le.AppendUint64(b, 44) // the size of the rest of the record
		b = le.AppendUint16(b, version45)
		b = le.AppendUint16(b, version45)
		b = le.AppendUint32(b, 0) // this disk
		b = le.AppendUint32(b, 0) // the disk where the directory starts
		b = le.AppendUint64(b, n) // entries on this disk
		b = le.AppendUint64(b, n) // entries in all
		b = le.AppendUint64(b, dirLen)
		b = le.AppendUint64(b, dir)

		b = le.AppendUint32(b, locator64Sig)
		b = le.AppendUint32(b, 0) // the disk of the Zip64 end record
		b = le.AppendUint64(b, dir+dirLen)
		b = le.AppendUint32(b, 1) // disks in all
		// All of them, so a reader finds the Zip64 record whichever field it checks.
		n16, dirLen32, dir32 = uint16max, uint32max, uint32max
	}
	b = le.AppendUint32(b, endSig)
	b = le.AppendUint16(b, 0) // this disk
	b = le.AppendUint16(b, 0) // the disk where the directory starts
	b = le.AppendUint16(b, n16)
	b = le.AppendUint16(b, n16)
	b = le.AppendUint32(b, dirLen32)
	b = le.AppendUint32(b, dir32)
	return le.AppendUint16(b, 0) // comment length
}

// Reader reads an archive. It asks its Source for a checksum only when a header needs one,
// and opens a file only when its data is read, one file at a time. Seek is O(1). Close
// closes the file it has open.
type Reader struct {
	a   *Archive
	ctx context.Context
	src Source
	pos int64

	crc   []uint32 // the checksums known so far,
	known []bool   // and which ones those are

	local      []byte // the local header encoded last,
	localEntry int    // and whose it is, or -1

	dir []byte // the central directory, once it is needed

	file      File // the open file, or nil,
	fileEntry int  // and whose it is
}

// Reader returns a Reader at the start of the archive. ctx is passed on to src.CRC32.
func (a *Archive) Reader(ctx context.Context, src Source) *Reader {
	return &Reader{
		a: a, ctx: ctx, src: src,
		crc: make([]uint32, len(a.entries)), known: make([]bool, len(a.entries)),
		localEntry: -1,
	}
}

// Read reads from the part of the archive at the current position: a local header, a
// file's data, the central directory or the end records. A file's data needs no checksum,
// so resuming a download in the middle of a file asks for none.
func (r *Reader) Read(p []byte) (int, error) {
	a := r.a
	if r.pos >= a.size {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	var part []byte // the rest of the part at pos, when it is in memory
	switch end := a.dir + a.dirLen; {
	case r.pos >= end:
		part = a.end[r.pos-end:]
	case r.pos >= a.dir:
		dir, err := r.directory()
		if err != nil {
			return 0, err
		}
		part = dir[r.pos-a.dir:]
	default:
		i := sort.Search(len(a.entries), func(i int) bool { return a.entries[i].offset > r.pos }) - 1
		e := &a.entries[i]
		off := r.pos - e.offset
		if off >= e.localLen() {
			n, err := r.readData(i, p, off-e.localLen())
			r.pos += int64(n)
			return n, err
		}
		h, err := r.localHeader(i)
		if err != nil {
			return 0, err
		}
		part = h[off:]
	}
	n := copy(p, part)
	r.pos += int64(n)
	return n, nil
}

// Seek sets where the next Read starts. Past the end is allowed; Read then says io.EOF.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.pos
	case io.SeekEnd:
		offset += r.a.size
	default:
		return 0, errors.New("zipstream: invalid whence")
	}
	if offset < 0 {
		return 0, errors.New("zipstream: negative position")
	}
	r.pos = offset
	return offset, nil
}

// Close closes the file the Reader has open, if any.
func (r *Reader) Close() error {
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

func (r *Reader) checksum(i int) (uint32, error) {
	if !r.known[i] {
		crc, err := r.src.CRC32(r.ctx, i)
		if err != nil {
			return 0, fmt.Errorf("zipstream: checksum of %s: %w", r.a.entries[i].name, err)
		}
		r.crc[i], r.known[i] = crc, true
	}
	return r.crc[i], nil
}

func (r *Reader) localHeader(i int) ([]byte, error) {
	if r.localEntry != i {
		crc, err := r.checksum(i)
		if err != nil {
			return nil, err
		}
		r.local, r.localEntry = r.a.entries[i].appendLocal(r.local[:0], crc), i
	}
	return r.local, nil
}

// directory builds the central directory, the one part that needs every checksum, once.
func (r *Reader) directory() ([]byte, error) {
	if r.dir == nil {
		dir := make([]byte, 0, r.a.dirLen)
		for i := range r.a.entries {
			crc, err := r.checksum(i)
			if err != nil {
				return nil, err
			}
			dir = r.a.entries[i].appendCentral(dir, crc)
		}
		r.dir = dir
	}
	return r.dir, nil
}

// readData reads entry i's data from off on, into p and no further than the data's end.
func (r *Reader) readData(i int, p []byte, off int64) (int, error) {
	e := &r.a.entries[i]
	if rest := e.size - off; int64(len(p)) > rest {
		p = p[:rest]
	}
	if r.file != nil && r.fileEntry != i {
		r.file.Close() // it was only read, so there is nothing to lose
		r.file = nil
	}
	if r.file == nil {
		f, err := r.src.Open(i)
		if err != nil {
			return 0, fmt.Errorf("zipstream: %s: %w", e.name, err)
		}
		r.file, r.fileEntry = f, i
	}
	n, err := r.file.ReadAt(p, off)
	switch {
	case n == len(p):
		return n, nil // ReadAt may report io.EOF with the last bytes
	case err == nil || err == io.EOF:
		// Zeros in place of the rest would make an archive that looks whole but isn't.
		return n, fmt.Errorf("zipstream: %s is shorter than %d bytes: %w", e.name, e.size, io.ErrUnexpectedEOF)
	default:
		return n, fmt.Errorf("zipstream: %s: %w", e.name, err)
	}
}
