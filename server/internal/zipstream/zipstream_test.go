package zipstream

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// rome is the server's usual time zone in summer, fixed so the tests need no tzdata.
var rome = time.FixedZone("CEST", 2*60*60)

// memSource keeps its files in memory and remembers what the archive asked of it.
type memSource struct {
	data  [][]byte
	crcs  []int // the entries whose checksums were asked for, in order
	opens []int
	open  int // files open now
	most  int // the most that were open at once
}

func (s *memSource) CRC32(ctx context.Context, i int) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.crcs = append(s.crcs, i)
	return crc32.ChecksumIEEE(s.data[i]), nil
}

func (s *memSource) Open(i int) (File, error) {
	s.opens = append(s.opens, i)
	s.open++
	s.most = max(s.most, s.open)
	return memFile{bytes.NewReader(s.data[i]), s}, nil
}

type memFile struct {
	*bytes.Reader
	s *memSource
}

// ReadAt says io.EOF along with the last bytes, as io.ReaderAt allows.
func (f memFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.Reader.ReadAt(p, off)
	if err == nil && off+int64(n) == f.Size() {
		err = io.EOF
	}
	return n, err
}

func (f memFile) Close() error {
	f.s.open--
	return nil
}

// days is a few days of uploads: a photo and its namesake, a video from just before
// midnight in Rome, a letter with an umlaut in its name, an empty file, and a recipe one
// folder deeper.
func days() ([]Entry, *memSource) {
	rng := rand.NewChaCha8([32]byte{1})
	files := []struct {
		name string
		size int
		t    time.Time
	}{
		{"2026-09-27/IMG_0001.jpg", 3000, time.Date(2026, 9, 27, 8, 15, 31, 250e6, time.UTC)},
		{"2026-09-27/IMG_0001 (2).jpg", 5000, time.Date(2026, 9, 27, 8, 15, 32, 0, time.UTC)},
		{"2026-09-27/VID_0002.mp4", 7000, time.Date(2026, 9, 27, 21, 59, 59, 0, time.UTC)},
		{"2026-09-28/Kündigung.pdf", 4096, time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)},
		{"2026-09-28/empty.txt", 0, time.Date(2026, 9, 28, 11, 0, 1, 0, time.UTC)},
		{"2026-09-30/Rezepte/Omas Kuchen.txt", 1234, time.Date(2026, 9, 30, 19, 45, 7, 0, time.UTC)},
	}
	src := &memSource{}
	entries := make([]Entry, len(files))
	for i, f := range files {
		b := make([]byte, f.size)
		rng.Read(b)
		src.data = append(src.data, b)
		entries[i] = Entry{Name: f.name, Size: int64(f.size), Modified: f.t}
	}
	return entries, src
}

// archive lays out entries and reads the whole archive.
func archive(t *testing.T, entries []Entry, src Source) (*Archive, []byte) {
	t.Helper()
	a, err := New(entries, rome)
	if err != nil {
		t.Fatal(err)
	}
	r := a.Reader(context.Background(), src)
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return a, data
}

// wantSize works out an archive's size from the rules of the layout, apart from the code.
func wantSize(entries []Entry) int64 {
	const m32 = 0xFFFFFFFF
	var off, dir int64
	for _, e := range entries {
		n := int64(len(e.Name))
		local, k := 30+n+9, int64(0)
		if e.Size >= m32 {
			local += 20
			k += 2
		}
		if off >= m32 {
			k++
		}
		dir += 46 + n + 9
		if k > 0 {
			dir += 4 + 8*k
		}
		off += local + e.Size
	}
	size := off + dir + 22
	if len(entries) >= 0xFFFF || dir >= m32 || off >= m32 {
		size += 56 + 20
	}
	return size
}

func TestRoundTrip(t *testing.T) {
	entries, src := days()
	a, err := New(entries, rome)
	if err != nil {
		t.Fatal(err)
	}
	if want := wantSize(entries); a.Size() != want {
		t.Errorf("Size() = %d, want %d", a.Size(), want)
	}
	r := a.Reader(context.Background(), src)
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) != a.Size() {
		t.Fatalf("read %d bytes, Size() = %d", len(data), a.Size())
	}
	if err := r.Close(); err != nil || src.open != 0 || src.most != 1 {
		t.Errorf("Close: %v; %d files still open, at most %d at once", err, src.open, src.most)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != len(entries) {
		t.Fatalf("%d files in the archive, want %d", len(zr.File), len(entries))
	}
	for i, f := range zr.File {
		e := entries[i]
		if f.Name != e.Name || f.UncompressedSize64 != uint64(e.Size) || f.Method != zip.Store || f.NonUTF8 {
			t.Errorf("file %d: %q of %d bytes, method %d, NonUTF8 %v; want %q of %d bytes, stored",
				i, f.Name, f.UncompressedSize64, f.Method, f.NonUTF8, e.Name, e.Size)
		}
		// archive/zip takes the time from the extended timestamp, and the zone from how far
		// the MS-DOS time, which is Rome's, is from it.
		if _, zone := f.Modified.Zone(); !f.Modified.Equal(e.Modified.Truncate(time.Second)) || zone != 2*60*60 {
			t.Errorf("%s: modified %v, want %v", e.Name, f.Modified, e.Modified.In(rome))
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc) // checks the CRC-32 at the end
		rc.Close()
		if err != nil || !bytes.Equal(got, src.data[i]) {
			t.Errorf("%s: %d bytes back, %v; want the %d put in", e.Name, len(got), err, e.Size)
		}
	}
}

func TestSize(t *testing.T) {
	a, err := New([]Entry{
		{Name: "2026-09-27/IMG_0001.jpg", Size: 3 << 20},
		{Name: "2026-09-27/IMG_0001 (2).jpg", Size: 3 << 20},
	}, rome)
	if err != nil {
		t.Fatal(err)
	}
	if a.Size() != 6_291_766 {
		t.Errorf("two photos of 3 MiB: %d bytes, want 6291766", a.Size())
	}

	_, data := archive(t, nil, &memSource{})
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(data) != 22 || len(zr.File) != 0 {
		t.Errorf("no files: %d bytes, %v", len(data), err)
	}
}

// A streaming unzipper like Java's ZipInputStream knows only the local headers: the sizes
// and CRC-32 must be in them, and bit 3 of the flags, which would put them after the data,
// clear. Bit 11 says the names are UTF-8.
func TestRawHeaders(t *testing.T) {
	entries, src := days()
	a, data := archive(t, entries, src)
	l := walk(t, bytes.NewReader(data))
	if len(l.locals) != len(entries) || len(l.centrals) != len(entries) {
		t.Fatalf("%d local and %d central headers, want %d of each", len(l.locals), len(l.centrals), len(entries))
	}
	for i, h := range l.locals {
		c, e := l.centrals[i], entries[i]
		if h.Flags&0x0800 == 0 || h.Flags&0x0008 != 0 || c.Flags&0x0800 == 0 || c.Flags&0x0008 != 0 {
			t.Errorf("%s: flags %#04x in the local header, %#04x in the central one", e.Name, h.Flags, c.Flags)
		}
		crc := crc32.ChecksumIEEE(data[h.data : h.data+e.Size])
		if h.name != e.Name || h.Method != 0 || h.CSize != uint32(e.Size) || h.USize != h.CSize || h.CRC != crc {
			t.Errorf("local header of %s: %q, %+v; want CRC-32 %#08x", e.Name, h.name, h.localHeader, crc)
		}
		if c.name != h.name || c.CRC != h.CRC || c.USize != h.USize || c.Offset != uint32(h.off) || c.Time != h.Time || c.Date != h.Date {
			t.Errorf("central header of %s: %q, %+v", e.Name, c.name, c.centralHeader)
		}
	}
	checkEnd(t, bytes.NewReader(data), a.Size(), len(entries), l)
}

func TestSeekAndRead(t *testing.T) {
	entries, src := days()
	a, whole := archive(t, entries, src)
	size := a.Size()
	r := a.Reader(context.Background(), src)
	defer r.Close()
	rng := rand.New(rand.NewPCG(1, 2))
	var pos int64
	for range 200 {
		off, n := rng.Int64N(size+100), rng.IntN(6000)
		var got int64
		var err error
		switch rng.IntN(3) {
		case 0:
			got, err = r.Seek(off, io.SeekStart)
		case 1:
			got, err = r.Seek(off-pos, io.SeekCurrent)
		default:
			got, err = r.Seek(off-size, io.SeekEnd)
		}
		if err != nil || got != off {
			t.Fatalf("Seek to %d: %d, %v", off, got, err)
		}
		buf := make([]byte, n)
		k, err := io.ReadFull(r, buf)
		want := whole[min(off, size):min(off+int64(n), size)]
		if !bytes.Equal(buf[:k], want) || (k < n) != (err != nil) {
			t.Fatalf("%d bytes at %d: %d, %v; want %d", n, off, k, err, len(want))
		}
		pos = off + int64(k)
	}

	for _, s := range []struct {
		offset int64
		whence int
	}{{-1, io.SeekStart}, {-pos - 1, io.SeekCurrent}, {-size - 1, io.SeekEnd}, {0, 3}} {
		if _, err := r.Seek(s.offset, s.whence); err == nil {
			t.Errorf("Seek(%d, %d) worked", s.offset, s.whence)
		}
	}
	if p, err := r.Seek(5, io.SeekEnd); err != nil || p != size+5 {
		t.Fatalf("Seek past the end: %d, %v", p, err)
	}
	if n, err := r.Read(make([]byte, 10)); n != 0 || err != io.EOF {
		t.Errorf("Read past the end: %d, %v", n, err)
	}
}

func TestChecksumsOnlyWhenNeeded(t *testing.T) {
	entries, src := days()
	a, data := archive(t, entries, src)
	l := walk(t, bytes.NewReader(data))
	src.crcs, src.opens = nil, nil

	r := a.Reader(context.Background(), src)
	defer r.Close()
	read := func(off, n int64) {
		t.Helper()
		if _, err := r.Seek(off, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(r, make([]byte, n)); err != nil {
			t.Fatal(err)
		}
	}
	// Resuming a download in the middle of a file needs that file, nothing else.
	read(l.locals[2].data+100, 1000)
	if len(src.crcs) != 0 || !slices.Equal(src.opens, []int{2}) {
		t.Errorf("reading data: checksums of %v, opened %v; want none and [2]", src.crcs, src.opens)
	}
	// A local header needs its own checksum.
	read(l.locals[3].off, 20)
	if !slices.Equal(src.crcs, []int{3}) {
		t.Errorf("reading a local header: checksums of %v, want [3]", src.crcs)
	}
	// The central directory needs the others, in order.
	read(l.dir, l.end-l.dir)
	if want := []int{3, 0, 1, 2, 4, 5}; !slices.Equal(src.crcs, want) {
		t.Errorf("reading the central directory: checksums of %v, want %v", src.crcs, want)
	}
	read(0, a.Size())
	if len(src.crcs) != len(entries) {
		t.Errorf("reading it all: checksums of %v; they were all known", src.crcs)
	}
}

func TestZip64(t *testing.T) {
	const gib = 1 << 30
	t0 := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		sizes []int64
	}{
		{"0xFFFFFFFE bytes", []int64{0xFFFFFFFE}},
		{"0xFFFFFFFF bytes", []int64{0xFFFFFFFF}},
		{"5 GiB", []int64{5 * gib}},
		{"a small file after 5 GiB", []int64{5 * gib, 10}},
		// The first local header takes 30 bytes, the name 20 and the timestamp 9, so the
		// second file starts at 0xFFFFFFFF, which only Zip64 can say.
		{"a file at 0xFFFFFFFF", []int64{0xFFFFFFFF - (30 + 20 + 9), 10}},
		{"65535 files", make([]int64, 65535)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := make([]Entry, len(tc.sizes))
			for i, s := range tc.sizes {
				entries[i] = Entry{Name: fmt.Sprintf("2026-09-27/%05d.mov", i), Size: s, Modified: t0} // 20 bytes
			}
			a, err := New(entries, rome)
			if err != nil {
				t.Fatal(err)
			}
			if want := wantSize(entries); a.Size() != want {
				t.Errorf("Size() = %d, want %d", a.Size(), want)
			}
			r := a.Reader(context.Background(), zeros(tc.sizes))
			defer r.Close()
			ra := readerAt{r}

			zr, err := zip.NewReader(ra, a.Size())
			if err != nil {
				t.Fatal(err)
			}
			l := walk(t, ra)
			if len(zr.File) != len(entries) || len(l.locals) != len(entries) || len(l.centrals) != len(entries) {
				t.Fatalf("%d files, %d local and %d central headers; want %d",
					len(zr.File), len(l.locals), len(l.centrals), len(entries))
			}
			for i, f := range zr.File {
				s := uint64(tc.sizes[i])
				data, err := f.DataOffset()
				if err != nil || f.Name != entries[i].Name || f.UncompressedSize64 != s || f.CompressedSize64 != s ||
					f.CRC32 != 0xDEADBEEF || data != l.locals[i].data {
					t.Errorf("%s: %q of %d bytes (%d compressed), CRC-32 %#08x, data at %d, %v; want %d bytes at %d",
						entries[i].Name, f.Name, f.UncompressedSize64, f.CompressedSize64, f.CRC32, data, err, s, l.locals[i].data)
				}
			}

			u64 := binary.LittleEndian.Uint64
			for i, h := range l.locals {
				s := uint64(tc.sizes[i])
				z := extraField(h.extra, 0x0001)
				if s >= 0xFFFFFFFF {
					if h.Version != 45 || h.CSize != 0xFFFFFFFF || h.USize != 0xFFFFFFFF || len(h.extra) != 29 ||
						len(z) != 16 || u64(z) != s || u64(z[8:]) != s {
						t.Errorf("local header of %s: %+v, extras %x", h.name, h.localHeader, h.extra)
					}
				} else if h.Version != 20 || h.CSize != uint32(s) || h.USize != uint32(s) || len(h.extra) != 9 {
					t.Errorf("local header of %s: %+v, extras %x", h.name, h.localHeader, h.extra)
				}
			}
			for i, h := range l.centrals {
				s, o := uint64(tc.sizes[i]), uint64(l.locals[i].off)
				var want []uint64 // the Zip64 fields: just the ones that don't fit
				if s >= 0xFFFFFFFF {
					want = append(want, s, s)
				}
				if o >= 0xFFFFFFFF {
					want = append(want, o)
				}
				z := extraField(h.extra, 0x0001)
				var got []uint64
				for b := z; len(b) >= 8; b = b[8:] {
					got = append(got, u64(b))
				}
				version := uint16(20)
				if want != nil {
					version = 45
				}
				if !slices.Equal(got, want) || len(z) != 8*len(want) || h.MadeBy != version || h.Version != version ||
					h.USize != uint32(min(s, 0xFFFFFFFF)) || h.CSize != h.USize || h.Offset != uint32(min(o, 0xFFFFFFFF)) {
					t.Errorf("central header of %s: %+v, extras %x; want Zip64 fields %d", h.name, h.centralHeader, h.extra, want)
				}
			}
			checkEnd(t, ra, a.Size(), len(entries), l)
		})
	}
}

func TestTimes(t *testing.T) {
	const last, lastDate = 23<<11 | 59<<5 | 29, 127<<9 | 12<<5 | 31 // 2107-12-31 23:59:58
	cases := []struct {
		t          time.Time
		time, date uint16 // MS-DOS, in Rome
		mtime      uint32
	}{
		// A quarter past midnight in Rome, still the day before in UTC.
		{time.Date(2026, 9, 27, 22, 15, 3, 0, time.UTC), 15<<5 | 1, 46<<9 | 9<<5 | 28, 1_790_547_303},
		{time.Date(1969, 7, 20, 20, 17, 40, 0, time.UTC), 0, 1<<5 | 1, 0},
		{time.Unix(10_000_000, 0), 0, 1<<5 | 1, 10_000_000},
		// The extended timestamp stops in 2038, where Java's and Android's signed reading does.
		{time.Date(2038, 1, 19, 3, 14, 8, 0, time.UTC), 5<<11 | 14<<5 | 4, 58<<9 | 1<<5 | 19, 0x7FFFFFFF},
		{time.Date(2107, 12, 31, 23, 59, 59, 0, rome), last, lastDate, 0x7FFFFFFF},
		{time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), last, lastDate, 0x7FFFFFFF},
	}
	entries := make([]Entry, len(cases))
	for i, tc := range cases {
		entries[i] = Entry{Name: fmt.Sprintf("%d.txt", i), Modified: tc.t}
	}
	_, data := archive(t, entries, &memSource{data: make([][]byte, len(cases))})
	l := walk(t, bytes.NewReader(data))
	for i, tc := range cases {
		for _, h := range []struct {
			kind       string
			time, date uint16
			extra      []byte
		}{
			{"local", l.locals[i].Time, l.locals[i].Date, l.locals[i].extra},
			{"central", l.centrals[i].Time, l.centrals[i].Date, l.centrals[i].extra},
		} {
			ts := extraField(h.extra, 0x5455)
			if h.time != tc.time || h.date != tc.date || len(ts) != 5 || ts[0] != 1 || binary.LittleEndian.Uint32(ts[1:]) != tc.mtime {
				t.Errorf("%v, %s header: MS-DOS time %#04x, date %#04x, extended timestamp %x; want %#04x, %#04x, %d",
					tc.t, h.kind, h.time, h.date, ts, tc.time, tc.date, tc.mtime)
			}
		}
	}
}

func TestNewRefusesBadNames(t *testing.T) {
	for _, names := range [][]string{
		{""},
		{"/abs"},
		{"a/../b"},
		{".."},
		{"a/./b"},
		{"a//b"},
		{"folder/"},
		{"a\\b"},
		{"x\x00y"},
		{"\xff.jpg"},
		{strings.Repeat("a", 65536)},
		{"2026-09-27/IMG_0001.jpg", "2026-09-27/IMG_0001.jpg"},
	} {
		entries := make([]Entry, len(names))
		for i, n := range names {
			entries[i] = Entry{Name: n}
		}
		if _, err := New(entries, rome); err == nil {
			t.Errorf("New took %.40q", names)
		}
	}
	if _, err := New([]Entry{{Name: strings.Repeat("a", 65535)}, {Name: "a/.b/c..d"}}, rome); err != nil {
		t.Errorf("New refused good names: %.40v", err)
	}
	if _, err := New([]Entry{{Name: "a", Size: -1}}, rome); err == nil {
		t.Error("New took a negative size")
	}
}

func TestReadErrors(t *testing.T) {
	entries, src := days()
	src.data[1] = src.data[1][:4000] // the file lost its last 1000 bytes
	a, err := New(entries, rome)
	if err != nil {
		t.Fatal(err)
	}
	r := a.Reader(context.Background(), src)
	defer r.Close()
	got, err := io.ReadAll(r)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !strings.Contains(err.Error(), entries[1].Name) {
		t.Fatalf("err = %v, want one about %s", err, entries[1].Name)
	}
	// The archive stops where the file does; nothing makes up for the missing bytes.
	start := int64(30+len(entries[0].Name)+9) + entries[0].Size + int64(30+len(entries[1].Name)+9)
	if int64(len(got)) != start+4000 || !bytes.Equal(got[start:], src.data[1]) {
		t.Errorf("read %d bytes before the error, want %d", len(got), start+4000)
	}
	if n, err := r.Read(make([]byte, 100)); n != 0 || err == nil {
		t.Errorf("Read after the error: %d, %v", n, err)
	}

	// A checksum that can't be had, here for a cancelled request, fails the read too.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Reader(ctx, src).Read(make([]byte, 100)); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// zeros is a Source of files that read as zeros, so an archive of any size takes no
// memory. Their checksums are made up; nothing here reads the data through.
type zeros []int64

func (z zeros) CRC32(context.Context, int) (uint32, error) { return 0xDEADBEEF, nil }
func (z zeros) Open(i int) (File, error)                   { return zeroFile(z[i]), nil }

type zeroFile int64

func (f zeroFile) ReadAt(p []byte, off int64) (int, error) {
	n := int(max(0, min(int64(len(p)), int64(f)-off)))
	clear(p[:n])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (zeroFile) Close() error { return nil }

// readerAt reads a Reader at any offset by seeking first, as an HTTP client asking for
// ranges does.
type readerAt struct{ r *Reader }

func (ra readerAt) ReadAt(p []byte, off int64) (int, error) {
	if _, err := ra.r.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.ReadFull(ra.r, p)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}

// localHeader and centralHeader are the fixed parts of the headers, as APPNOTE.TXT lays
// them out.
type localHeader struct {
	Sig                    uint32
	Version, Flags, Method uint16
	Time, Date             uint16
	CRC, CSize, USize      uint32
	NameLen, ExtraLen      uint16
}

type centralHeader struct {
	Sig                            uint32
	MadeBy, Version, Flags, Method uint16
	Time, Date                     uint16
	CRC, CSize, USize              uint32
	NameLen, ExtraLen, CommentLen  uint16
	Disk, Internal                 uint16
	External, Offset               uint32
}

type local struct {
	localHeader
	off, data int64 // where the header and the file's data start
	name      string
	extra     []byte
}

type central struct {
	centralHeader
	off   int64
	name  string
	extra []byte
}

type layout struct {
	locals   []local
	centrals []central
	dir, end int64 // where the central directory and the end records start
}

// walk reads an archive's headers the way unzip tools do: the local headers one after the
// other by the sizes they give, as a streaming unzipper must, then the central directory.
// It reads no file data.
func walk(t *testing.T, ra io.ReaderAt) layout {
	t.Helper()
	at := func(off int64, v any) {
		t.Helper()
		if err := binary.Read(io.NewSectionReader(ra, off, 1<<62), binary.LittleEndian, v); err != nil {
			t.Fatalf("reading at %d: %v", off, err)
		}
	}
	sig := func(off int64) uint32 {
		var s uint32
		at(off, &s)
		return s
	}
	rest := func(off int64, n int) []byte {
		t.Helper()
		b := make([]byte, n)
		if _, err := ra.ReadAt(b, off); err != nil {
			t.Fatalf("reading %d bytes at %d: %v", n, off, err)
		}
		return b
	}
	var l layout
	var off int64
	for sig(off) == 0x04034b50 {
		var h local
		at(off, &h.localHeader)
		b := rest(off+30, int(h.NameLen)+int(h.ExtraLen))
		h.off, h.data = off, off+30+int64(len(b))
		h.name, h.extra = string(b[:h.NameLen]), b[h.NameLen:]
		size := int64(h.CSize)
		if h.CSize == 0xFFFFFFFF {
			z := extraField(h.extra, 0x0001)
			if len(z) != 16 {
				t.Fatalf("local header of %s: Zip64 extra %x", h.name, z)
			}
			size = int64(binary.LittleEndian.Uint64(z[8:]))
		}
		l.locals = append(l.locals, h)
		off = h.data + size
	}
	l.dir = off
	for sig(off) == 0x02014b50 {
		var h central
		at(off, &h.centralHeader)
		n, x := int(h.NameLen), int(h.ExtraLen)
		b := rest(off+46, n+x+int(h.CommentLen))
		h.off, h.name, h.extra = off, string(b[:n]), b[n:n+x]
		l.centrals = append(l.centrals, h)
		off += 46 + int64(len(b))
	}
	l.end = off
	return l
}

// extraField finds an extra field's data by its tag.
func extraField(extra []byte, tag uint16) []byte {
	for len(extra) >= 4 {
		id, n := binary.LittleEndian.Uint16(extra), int(binary.LittleEndian.Uint16(extra[2:]))
		if 4+n > len(extra) {
			return nil
		}
		if id == tag {
			return extra[4 : 4+n]
		}
		extra = extra[4+n:]
	}
	return nil
}

// zip64End is the Zip64 end of central directory record and its locator.
type zip64End struct {
	Sig            uint32
	Len            uint64
	MadeBy, Needed uint16
	Disk, DirDisk  uint32
	Entries, Total uint64
	Size, Offset   uint64
	LocSig         uint32
	EndDisk        uint32
	End            uint64
	Disks          uint32
}

type endRecord struct {
	Sig            uint32
	Disk, DirDisk  uint16
	Entries, Total uint16
	Size, Offset   uint32
	CommentLen     uint16
}

// checkEnd checks the end records after the central directory that walk found. With the
// Zip64 ones, all fields of the old record say "look there".
func checkEnd(t *testing.T, ra io.ReaderAt, size int64, n int, l layout) {
	t.Helper()
	dir, dirLen := uint64(l.dir), uint64(l.end-l.dir)
	end := endRecord{Sig: 0x06054b50, Entries: uint16(n), Total: uint16(n), Size: uint32(dirLen), Offset: uint32(dir)}
	var want []byte
	if n >= 0xFFFF || dirLen >= 0xFFFFFFFF || dir >= 0xFFFFFFFF {
		want, _ = binary.Append(nil, binary.LittleEndian, zip64End{
			Sig: 0x06064b50, Len: 44, MadeBy: 45, Needed: 45, Entries: uint64(n), Total: uint64(n),
			Size: dirLen, Offset: dir, LocSig: 0x07064b50, End: dir + dirLen, Disks: 1,
		})
		end.Entries, end.Total, end.Size, end.Offset = 0xFFFF, 0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF
	}
	want, _ = binary.Append(want, binary.LittleEndian, end)
	if size-l.end != int64(len(want)) {
		t.Errorf("%d bytes after the central directory, want %d", size-l.end, len(want))
		return
	}
	got := make([]byte, len(want))
	if _, err := ra.ReadAt(got, l.end); err != nil || !bytes.Equal(got, want) {
		t.Errorf("end records %x, %v; want %x", got, err, want)
	}
}
