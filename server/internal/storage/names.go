package storage

import (
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eschgi/share/server/internal/db"
)

// maxNameBytes is the limit of ext4, exFAT and NTFS alike.
const maxNameBytes = 255

// SanitizeName turns a client's file name into one that is safe inside the library on
// ext4, exFAT and NTFS: no folders, no control characters, none of \/:*?"<>|, no trailing
// dots or spaces, no hidden names, no Windows device names, at most 255 bytes with the
// extension kept.
func SanitizeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == utf8.RuneError, r < 0x20, r == 0x7f:
			continue
		case strings.ContainsRune(`:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if s == "" {
		return "file"
	}
	if strings.HasPrefix(s, ".") {
		s = "_" + s
	}
	if stem, _, _ := strings.Cut(s, "."); isWindowsDeviceName(stem) {
		s = "_" + s
	}
	return truncateName(s)
}

func isWindowsDeviceName(stem string) bool {
	switch u := strings.ToUpper(strings.TrimSpace(stem)); u {
	case "CON", "PRN", "AUX", "NUL":
		return true
	default:
		return len(u) == 4 && (strings.HasPrefix(u, "COM") || strings.HasPrefix(u, "LPT")) && u[3] >= '1' && u[3] <= '9'
	}
}

// truncateName shortens a name to maxNameBytes on a rune boundary, keeping a short extension.
func truncateName(s string) string {
	if len(s) <= maxNameBytes {
		return s
	}
	stem, ext := splitExt(s)
	if len(ext) > 16 {
		stem, ext = s, ""
	}
	limit := maxNameBytes - len(ext)
	for len(stem) > limit {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return strings.TrimRight(stem, ". ") + ext
}

// splitExt splits "IMG_1.jpg" into "IMG_1" and ".jpg". A leading dot is not an extension.
func splitExt(name string) (stem, ext string) {
	ext = path.Ext(name)
	if ext == name {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// maxFolderName is how long a folder's name may be, in characters.
const maxFolderName = 60

// FolderName cleans a folder's name as typed: no control characters, spaces trimmed, at most
// 60 characters. It returns "" if nothing is left.
func FolderName(name string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(name) {
		if r == utf8.RuneError || r < 0x20 || r == 0x7f {
			continue
		}
		if n == maxFolderName {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// Numbered gives the n-th alternative for a taken name: "IMG_1 (2).jpg", still at most
// maxNameBytes long.
func Numbered(name string, n int) string { return numbered(name, n) }

func numbered(name string, n int) string {
	stem, ext := splitExt(name)
	if len(ext) > 16 {
		stem, ext = name, ""
	}
	suffix := " (" + strconv.Itoa(n) + ")"
	for len(stem)+len(suffix)+len(ext) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + suffix + ext
}

type fileType struct{ mime, kind string }

// knownTypes decides mime and kind by extension first: phones name their files reliably, and
// content sniffing can't tell a MOV from an MP4 or a DOCX from a ZIP.
var knownTypes = map[string]fileType{
	".jpg": {"image/jpeg", db.KindPhoto}, ".jpeg": {"image/jpeg", db.KindPhoto},
	".png": {"image/png", db.KindPhoto}, ".gif": {"image/gif", db.KindPhoto},
	".webp": {"image/webp", db.KindPhoto}, ".heic": {"image/heic", db.KindPhoto},
	".heif": {"image/heif", db.KindPhoto}, ".avif": {"image/avif", db.KindPhoto},
	".bmp": {"image/bmp", db.KindPhoto}, ".tif": {"image/tiff", db.KindPhoto},
	".tiff": {"image/tiff", db.KindPhoto}, ".dng": {"image/x-adobe-dng", db.KindPhoto},
	".mp4": {"video/mp4", db.KindVideo}, ".m4v": {"video/x-m4v", db.KindVideo},
	".mov": {"video/quicktime", db.KindVideo}, ".3gp": {"video/3gpp", db.KindVideo},
	".mkv": {"video/x-matroska", db.KindVideo}, ".webm": {"video/webm", db.KindVideo},
	".avi":  {"video/x-msvideo", db.KindVideo},
	".pdf":  {"application/pdf", db.KindDocument},
	".txt":  {"text/plain; charset=utf-8", db.KindDocument},
	".csv":  {"text/csv; charset=utf-8", db.KindDocument},
	".zip":  {"application/zip", db.KindDocument},
	".doc":  {"application/msword", db.KindDocument},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", db.KindDocument},
	".xls":  {"application/vnd.ms-excel", db.KindDocument},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", db.KindDocument},
	".ppt":  {"application/vnd.ms-powerpoint", db.KindDocument},
	".pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", db.KindDocument},
	".odt":  {"application/vnd.oasis.opendocument.text", db.KindDocument},
	".ods":  {"application/vnd.oasis.opendocument.spreadsheet", db.KindDocument},
	".mp3":  {"audio/mpeg", db.KindDocument}, ".m4a": {"audio/mp4", db.KindDocument},
	".wav": {"audio/wav", db.KindDocument},
}

// GuessKind is the kind a name suggests, used while a file is still arriving.
func GuessKind(name string) string {
	if t, ok := knownTypes[strings.ToLower(path.Ext(name))]; ok {
		return t.kind
	}
	return db.KindDocument
}

// Classify decides the stored mime type and kind from the name and, for unknown extensions,
// the first bytes. HTML and SVG are stored as plain bytes, so a download can never run as a
// page on this origin.
func Classify(name string, head []byte) (mime, kind string) {
	if t, ok := knownTypes[strings.ToLower(path.Ext(name))]; ok {
		return t.mime, t.kind
	}
	mime = http.DetectContentType(head)
	switch {
	case strings.HasPrefix(mime, "text/html"), strings.HasPrefix(mime, "text/xml"), strings.Contains(mime, "svg"):
		return "application/octet-stream", db.KindDocument
	case strings.HasPrefix(mime, "image/"):
		return mime, db.KindPhoto
	case strings.HasPrefix(mime, "video/"):
		return mime, db.KindVideo
	}
	return mime, db.KindDocument
}

// Day is the library folder, and the day a file is grouped under: the upload day in the
// configured time zone.
func Day(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(time.DateOnly)
}
