package storage

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/s3"
)

// In a bucket the bytes of every file are one object, named after the file's id. Folders,
// days, names and the trash are rows in the database only: moving, renaming, deleting and
// restoring change nothing in the bucket, and only a purge removes an object.

var (
	// ErrFinished means an upload can't be dropped: it is in the library, or on its way there.
	ErrFinished = errors.New("the upload is finished")
	// ErrUploadGone means the bucket doesn't have an unfinished upload any more, e.g. after
	// its own cleanup of old ones.
	ErrUploadGone = errors.New("the bucket doesn't have the upload any more")
)

// OpenS3Library opens the library in a bucket; layout has only the data folder.
func OpenS3Library(d *db.DB, b *s3.Bucket, layout Layout, loc *time.Location, now func() time.Time, logf func(string, ...any)) *Library {
	lib := &Library{DB: d, Layout: layout, Loc: loc, Now: now, Logf: logf, s3: b}
	for i := range lib.locks {
		lib.locks[i] = make(chan struct{}, 1)
	}
	return lib
}

// S3 is the bucket of a library in S3 mode, nil on a drive.
func (lib *Library) S3() *s3.Bucket { return lib.s3 }

// S3PartSize is the size of an upload's parts but the last: chunk, or for a file that would
// need more than s3.MaxParts of them the next whole MiB that does.
func S3PartSize(size, chunk int64) int64 {
	if size <= chunk*s3.MaxParts {
		return chunk
	}
	need := (size + s3.MaxParts - 1) / s3.MaxParts
	return (need + 1<<20 - 1) &^ (1<<20 - 1)
}

// S3PartCount is how many parts an upload of size bytes has; none for an empty file.
func S3PartCount(size, partSize int64) int { return int((size + partSize - 1) / partSize) }

// S3PartLen is the size of part n, counted from 1.
func S3PartLen(n int, size, partSize int64) int64 {
	return min(partSize, size-int64(n-1)*partSize)
}

// StartS3Upload starts the multipart upload of a new file. The object gets the type its name
// suggests and its name for downloads, which the links name again anyway.
func (lib *Library) StartS3Upload(ctx context.Context, id, name string) (string, error) {
	return lib.s3.CreateUpload(ctx, lib.s3.Key(id), objectType(name), ContentDisposition(name))
}

func objectType(name string) string {
	if mime, _, ok := TypeByName(name); ok {
		return mime
	}
	return "application/octet-stream"
}

// lockUpload waits until nothing else finishes or drops upload id. The locks are taken before
// lib.mu, never the other way round, and no call to the bucket is made while lib.mu is held.
func (lib *Library) lockUpload(ctx context.Context, id string) (func(), error) {
	h := fnv.New32a()
	h.Write([]byte(id))
	l := lib.locks[h.Sum32()%uint32(len(lib.locks))]
	select {
	case l <- struct{}{}:
		return func() { <-l }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// finalizeS3 is Finalize in a bucket: once every part is there with its size, the multipart
// upload becomes the object, and the file goes into the library as on a drive.
func (lib *Library) finalizeS3(ctx context.Context, id string) error {
	unlock, err := lib.lockUpload(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := lib.DB.FileByID(ctx, id)
	if err != nil {
		return err
	}
	switch f.State {
	case db.StateReady, db.StateTrashed:
		return nil
	case db.StateReceiving:
		if err := lib.completeS3(ctx, f); err != nil {
			return err
		}
		lib.mu.Lock()
		f, err = lib.claimPath(ctx, f)
		lib.mu.Unlock()
		if err != nil {
			return err
		}
		if f.State != db.StateFinalizing {
			return nil // finished meanwhile
		}
	}
	mime, kind, err := lib.classifyS3(ctx, f)
	if err != nil {
		return err
	}
	if err := lib.DB.MarkReady(ctx, id, mime, kind, lib.Now()); err != nil {
		return err
	}
	if lib.OnReady != nil {
		lib.OnReady(id)
	}
	return nil
}

// completeS3 makes the object of a receiving upload: ErrIncomplete while a part is missing
// or has the wrong size. An empty file needs no upload. When the bucket doesn't know the
// upload any more, an object of the right size means it was completed already, e.g. by a
// request whose answer got lost.
func (lib *Library) completeS3(ctx context.Context, f db.File) error {
	key := lib.s3.Key(f.ID)
	if f.Size == 0 {
		return lib.s3.PutEmpty(ctx, key, objectType(f.Name), ContentDisposition(f.Name))
	}
	parts, err := lib.s3.Parts(ctx, key, f.S3UploadID)
	if errors.Is(err, s3.ErrNoUpload) {
		return lib.checkObject(ctx, f)
	}
	if err != nil {
		return err
	}
	n := S3PartCount(f.Size, f.S3PartSize)
	list := make([]s3.Part, 0, n)
	for i := 1; i <= n; i++ {
		p, ok := parts[i]
		if !ok || p.Size != S3PartLen(i, f.Size, f.S3PartSize) {
			return ErrIncomplete
		}
		list = append(list, p)
	}
	switch err := lib.s3.Complete(ctx, key, f.S3UploadID, list); {
	case errors.Is(err, s3.ErrInvalidPart):
		return ErrIncomplete // a part was sent again meanwhile
	case err != nil && !errors.Is(err, s3.ErrNoUpload):
		return err
	}
	return lib.checkObject(ctx, f)
}

// checkObject makes sure an upload became its object, with all its bytes.
func (lib *Library) checkObject(ctx context.Context, f db.File) error {
	size, err := lib.s3.Stat(ctx, lib.s3.Key(f.ID))
	switch {
	case errors.Is(err, s3.ErrNoObject):
		return ErrUploadGone
	case err != nil:
		return err
	case size != f.Size:
		return fmt.Errorf("finalize %s: the object has %d bytes instead of %d", f.ID, size, f.Size)
	}
	return nil
}

// classifyS3 is classify for an object: by the name, else by its first bytes. A failed read
// is an error, never a guess.
func (lib *Library) classifyS3(ctx context.Context, f db.File) (mime, kind string, err error) {
	if mime, kind, ok := TypeByName(f.Name); ok {
		return mime, kind, nil
	}
	var head []byte
	if f.Size > 0 {
		if head, err = lib.s3.Head(ctx, lib.s3.Key(f.ID), 512); err != nil {
			return "", "", err
		}
	}
	mime, kind = Classify(f.Name, head)
	return mime, kind, nil
}

// terminateS3 is Terminate in a bucket: the parts go, then the row. An upload that finished
// meanwhile stays, and ErrFinished says so.
func (lib *Library) terminateS3(ctx context.Context, id string) error {
	unlock, err := lib.lockUpload(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := lib.DB.FileByID(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.State != db.StateReceiving {
		return ErrFinished
	}
	if f.S3UploadID != "" {
		if err := lib.s3.Abort(ctx, lib.s3.Key(id), f.S3UploadID); err != nil {
			// The reconciler aborts uploads without a row, and buckets drop old ones anyway.
			lib.Logf("storage: aborting the upload of %s: %v", id, err)
		}
	}
	return lib.DB.DeleteFileRow(ctx, id)
}

// purgeS3 is Purge in a bucket: the rows go at once, each object noted for removal in the
// same transaction, and the objects follow once the database is free again.
func (lib *Library) purgeS3(ctx context.Context, fileIDs []string) ([]db.File, error) {
	out, err := func() ([]db.File, error) {
		lib.mu.Lock()
		defer lib.mu.Unlock()
		files, err := lib.DB.TrashedByID(ctx, fileIDs)
		if err != nil {
			return nil, err
		}
		var out []db.File
		for _, f := range files {
			ok, err := lib.DB.PurgeToS3Garbage(ctx, f.ID, lib.s3.Key(f.ID), lib.Now())
			if err != nil {
				return out, err
			}
			if !ok {
				continue
			}
			if lib.OnPurged != nil {
				lib.OnPurged(f.ID)
			}
			out = append(out, f)
		}
		return out, lib.dropEmptyFolders(ctx)
	}()
	if gerr := lib.emptyS3Garbage(ctx); gerr != nil {
		lib.Logf("storage: removing deleted files from the bucket: %v; trying again later", gerr)
	}
	return out, err
}

// emptyS3Garbage removes the objects of purged files. What can't be removed now stays noted.
func (lib *Library) emptyS3Garbage(ctx context.Context) error {
	for {
		keys, err := lib.DB.S3Garbage(ctx, 100)
		if err != nil || len(keys) == 0 {
			return err
		}
		for _, key := range keys {
			if err := lib.s3.Remove(ctx, key); err != nil {
				return fmt.Errorf("removing %s: %w", key, err)
			}
			if err := lib.DB.ForgetS3Garbage(ctx, key); err != nil {
				return err
			}
		}
	}
}

// reconcileS3 is Reconcile in a bucket:
//   - renames and moves that a crash cut short are noted as done: in a bucket nothing moves;
//   - uploads stuck halfway through finalizing are finished;
//   - uploads idle for longer than ttl are finished if all their parts are there, else
//     dropped with their parts;
//   - an empty file whose row a crash left receiving is finished after an hour;
//   - uploads in the bucket without a row are aborted after an hour;
//   - objects of purged files still in the bucket are removed;
//   - files that reached a deleted folder go to the trash, and empty deleted folders are
//     forgotten.
//
// The trash needs nothing: its files stay where they are. While the bucket can't be reached,
// what needs it waits for the next round.
func (lib *Library) reconcileS3(ctx context.Context, ttl time.Duration) error {
	if err := lib.relocate(ctx); err != nil {
		return err
	}
	if err := lib.finishMoves(ctx); err != nil {
		return err
	}
	bucket, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	now := lib.Now()
	rows, err := lib.DB.FilesInStates(ctx, db.StateFinalizing, db.StateReceiving)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range rows {
		known[f.S3UploadID] = true
		var err error
		switch {
		case f.State == db.StateFinalizing:
			err = lib.Finalize(bucket, f.ID)
		case now.Sub(f.UpdatedAt) > ttl:
			err = lib.Finalize(bucket, f.ID)
			if errors.Is(err, ErrIncomplete) || errors.Is(err, ErrUploadGone) {
				lib.Logf("storage: dropping %s (%q), idle since %s", f.ID, f.Name, f.UpdatedAt.Format(time.RFC3339))
				err = lib.Terminate(bucket, f.ID)
			}
		case f.S3UploadID == "" && now.Sub(f.CreatedAt) > time.Hour:
			err = lib.Finalize(bucket, f.ID)
		}
		if err != nil && !errors.Is(err, ErrFinished) {
			lib.Logf("storage: finishing %s: %v", f.ID, err)
		}
	}
	if err := lib.s3.Uploads(bucket, func(u s3.Upload) error {
		if known[u.UploadID] || now.Sub(u.Initiated) < time.Hour {
			return nil
		}
		lib.Logf("storage: aborting %s in the bucket, an upload without a file since %s", u.Key, u.Initiated.Format(time.RFC3339))
		return lib.s3.Abort(bucket, u.Key, u.UploadID)
	}); err != nil {
		lib.Logf("storage: aborting uploads without a file: %v", err)
	}
	if err := lib.emptyS3Garbage(bucket); err != nil {
		lib.Logf("storage: removing deleted files from the bucket: %v", err)
	}
	if err := lib.sweepDeletedFolders(ctx); err != nil {
		return err
	}
	return lib.dropEmptyFolders(ctx)
}
