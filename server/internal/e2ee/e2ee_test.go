package e2ee

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `go test ./internal/e2ee -update` writes new vectors into contract/crypto, from new random
// keys; the website's and the app's tests check their code against them.
var update = flag.Bool("update", false, "write new vectors into contract/crypto")

var b64 = base64.RawURLEncoding

func vectorPath(name string) string {
	return filepath.Join("..", "..", "..", "contract", "crypto", name)
}

func readVectors(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(vectorPath(name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func writeVectors(t *testing.T, name string, v any) {
	t.Helper()
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false) // the descriptions' <folder> stay readable
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vectorPath(name), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The suite is the one of RFC 9180's vector: its keys, and its first encryption opens.
func TestRFC9180Vector(t *testing.T) {
	var v map[string]any
	readVectors(t, "hpke_rfc9180.json", &v)
	get := func(k string) []byte { return unhex(t, v[k].(string)) }
	kem, kdf, aead := suite()
	if kem.ID() != uint16(v["kem_id"].(float64)) || kdf.ID() != uint16(v["kdf_id"].(float64)) || aead.ID() != uint16(v["aead_id"].(float64)) {
		t.Fatal("not the vector's suite")
	}
	pub, err := PublicKey(get("skRm"))
	if err != nil || !bytes.Equal(pub, get("pkRm")) {
		t.Fatalf("public key: %x, %v", pub, err)
	}
	if pub, _ := PublicKey(get("skEm")); !bytes.Equal(pub, get("enc")) {
		t.Fatalf("enc is the ephemeral public key: %x", pub)
	}
	pt, err := Open(get("skRm"), string(get("info")), get("aad"), append(get("enc"), get("ct")...))
	if err != nil || !bytes.Equal(pt, get("pt")) {
		t.Fatalf("open: %x, %v", pt, err)
	}
}

func TestSealAndOpen(t *testing.T) {
	priv, pub := GenerateKey()
	aad := FolderContext("w3dding5x2k7mbqz4bwdbyj6qs", 2)
	key := make([]byte, FileKeySize)
	rand.Read(key)
	sealed, err := Seal(pub, PurposeFile, aad, key)
	if err != nil || len(sealed) != FileKeySize+SealOverhead {
		t.Fatalf("seal: %d bytes, %v", len(sealed), err)
	}
	if got, err := Open(priv, PurposeFile, aad, sealed); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("open: %x, %v", got, err)
	}
	other, _ := GenerateKey()
	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	for name, try := range map[string]func() ([]byte, error){
		"another purpose": func() ([]byte, error) { return Open(priv, PurposeFolder, aad, sealed) },
		"another folder": func() ([]byte, error) {
			return Open(priv, PurposeFile, FolderContext("w3dding5x2k7mbqz4bwdbyj6qs", 3), sealed)
		},
		"another key":   func() ([]byte, error) { return Open(other, PurposeFile, aad, sealed) },
		"a flipped bit": func() ([]byte, error) { return Open(priv, PurposeFile, aad, flipped) },
		"cut short":     func() ([]byte, error) { return Open(priv, PurposeFile, aad, sealed[:40]) },
	} {
		if _, err := try(); err == nil {
			t.Errorf("%s opens", name)
		}
	}
	// What Go's own HPKE seals opens here.
	kem, kdf, aead := suite()
	pk, _ := kem.NewPublicKey(pub)
	enc, s, _ := hpke.NewSender(pk, kdf, aead, []byte(PurposeFile))
	ct, _ := s.Seal(aad, key)
	if got, err := Open(priv, PurposeFile, aad, append(enc, ct...)); err != nil || !bytes.Equal(got, key) {
		t.Errorf("open what crypto/hpke sealed: %v", err)
	}
	if CheckPublicKey(pub) != nil || CheckPublicKey(pub[1:]) == nil || CheckPublicKey(make([]byte, PublicKeySize)) == nil {
		t.Error("CheckPublicKey")
	}
}

func TestLocks(t *testing.T) {
	key := SecretKey([]byte("a secret from a link"), PurposeInvite)
	locked := Lock(key, []byte("person:u1"), []byte("folder key"))
	if got, err := Unlock(key, []byte("person:u1"), locked); err != nil || string(got) != "folder key" {
		t.Fatalf("unlock: %q, %v", got, err)
	}
	if _, err := Unlock(key, []byte("person:u2"), locked); err == nil {
		t.Error("unlocks with another aad")
	}
	if _, err := Unlock(SecretKey([]byte("a secret from a link"), PurposePin), []byte("person:u1"), locked); err == nil {
		t.Error("unlocks with the key for another purpose")
	}
	priv, _ := GenerateKey()
	pl := passwordLockWith(rand.Reader, "correct horse", PasswordIterations/10, PersonContext("u1"), priv)
	if CheckPasswordLock(pl) != nil {
		t.Error("CheckPasswordLock")
	}
	if got, err := PasswordUnlock("correct horse", PersonContext("u1"), pl); err != nil || !bytes.Equal(got, priv) {
		t.Fatalf("password unlock: %v", err)
	}
	if _, err := PasswordUnlock("Correct horse", PersonContext("u1"), pl); err == nil {
		t.Error("unlocks with another password")
	}
	weak := passwordLockWith(rand.Reader, "correct horse", 1000, PersonContext("u1"), priv)
	if _, err := PasswordUnlock("correct horse", PersonContext("u1"), weak); err == nil || CheckPasswordLock(weak) == nil {
		t.Error("takes a lock with 1000 iterations")
	}
}

func plaintext(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func TestContent(t *testing.T) {
	key := make([]byte, FileKeySize)
	rand.Read(key)
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 77} {
		h := NewHeader()
		plain := plaintext(n)
		enc := Encrypt(key, h, plain)
		if int64(len(enc)) != EncryptedSize(int64(n)) {
			t.Fatalf("%d bytes: %d encrypted, want %d", n, len(enc), EncryptedSize(int64(n)))
		}
		if got, err := Decrypt(key, enc, int64(n)); err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d bytes: decrypt: %v", n, err)
		}
		if !bytes.Equal(Encrypt(key, h, plain), enc) {
			t.Errorf("%d bytes: not the same twice", n)
		}
	}
	h := NewHeader()
	plain := plaintext(3 * ChunkSize)
	enc := Encrypt(key, h, plain)
	c := ChunkSize + tagSize
	swapped := bytes.Clone(enc)
	copy(swapped[HeaderSize:], enc[HeaderSize+c:HeaderSize+2*c])
	copy(swapped[HeaderSize+c:], enc[HeaderSize:HeaderSize+c])
	other := NewHeader()
	for name, try := range map[string]func() ([]byte, error){
		"swapped chunks":   func() ([]byte, error) { return Decrypt(key, swapped, int64(len(plain))) },
		"a chunk cut off":  func() ([]byte, error) { return Decrypt(key, enc[:HeaderSize+2*c], 2*ChunkSize) },
		"another header":   func() ([]byte, error) { return Decrypt(key, append(other[:], enc[HeaderSize:]...), int64(len(plain))) },
		"another file key": func() ([]byte, error) { return Decrypt(make([]byte, FileKeySize), enc, int64(len(plain))) },
	} {
		if _, err := try(); err == nil {
			t.Errorf("%s decrypt", name)
		}
	}
	if _, err := ParseHeader(append([]byte("SHE2"), h[4:]...)); err == nil {
		t.Error("ParseHeader takes another version")
	}
	thumb := SealThumb(key, []byte("a JPEG"))
	if got, err := OpenThumb(key, thumb); err != nil || string(got) != "a JPEG" {
		t.Errorf("thumb: %q, %v", got, err)
	}
}

func TestRecoveryCode(t *testing.T) {
	secret, code := NewRecoveryCode()
	if len(code) != 39 || strings.Count(code, "-") != 7 {
		t.Fatalf("code %q", code)
	}
	if got, err := ParseRecoveryCode(code); err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("parse %q: %x, %v", code, got, err)
	}
	if got, _ := ParseRecoveryCode(strings.ToLower(strings.ReplaceAll(code, "-", " "))); !bytes.Equal(got, secret) {
		t.Error("small letters and spaces")
	}
	for _, bad := range []string{code[:len(code)-1], code + "0", strings.Replace(code, code[:1], "U", 1)} {
		if _, err := ParseRecoveryCode(bad); err == nil {
			t.Errorf("reads %q", bad)
		}
	}
}

// The vectors: what Share seals, locks and encrypts, for the website's and the app's tests.

type sealCase struct {
	Name       string `json:"name"`
	Purpose    string `json:"purpose"`
	AAD        string `json:"aad"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	Plaintext  string `json:"plaintext"`
	Sealed     string `json:"sealed"`
}

type sealVectors struct {
	Description string     `json:"description"`
	Open        []sealCase `json:"open"`
	Refuse      []sealCase `json:"refuse"`
}

type secretCase struct {
	Name      string `json:"name"`
	Purpose   string `json:"purpose"`
	Secret    string `json:"secret"`
	Key       string `json:"key"`
	AAD       string `json:"aad"`
	Plaintext string `json:"plaintext"`
	Locked    string `json:"locked"`
}

type passwordCase struct {
	Password  string `json:"password"`
	AAD       string `json:"aad"`
	Plaintext string `json:"plaintext"`
	Locked    string `json:"locked"`
}

type thumbCase struct {
	FileKey   string `json:"file_key"`
	ThumbKey  string `json:"thumb_key"`
	Plaintext string `json:"plaintext"`
	Sealed    string `json:"sealed"`
}

type lockVectors struct {
	Description string         `json:"description"`
	Secrets     []secretCase   `json:"secrets"`
	Passwords   []passwordCase `json:"passwords"`
	Thumb       thumbCase      `json:"thumb"`
}

type contentCase struct {
	Size          int64  `json:"size"`
	EncryptedSize int64  `json:"encrypted_size"`
	SHA256        string `json:"sha256"`
	Encrypted     string `json:"encrypted,omitempty"`
}

type contentVectors struct {
	Description string        `json:"description"`
	FileKey     string        `json:"file_key"`
	Header      string        `json:"header"`
	ContentKey  string        `json:"content_key"`
	Cases       []contentCase `json:"cases"`
}

type recoveryVectors struct {
	Description string            `json:"description"`
	Secret      string            `json:"secret"`
	Code        string            `json:"code"`
	AlsoReads   []string          `json:"also_reads"`
	Refuses     []string          `json:"refuses"`
	Key         string            `json:"key"`
	Lock        map[string]string `json:"lock"`
}

// checkCase is a check of contract/crypto/check.json: both one-time key pairs, the key that gets
// the keys, and what each side makes of them.
type checkCase struct {
	Name          string `json:"name"`
	Check         string `json:"check"`
	AskerPrivate  string `json:"asker_private_key"`
	AskerKey      string `json:"asker_key"`
	AnswerPrivate string `json:"answer_private_key"`
	AnswerKey     string `json:"answer_key"`
	PublicKey     string `json:"public_key"`
	Commitment    string `json:"commitment"`
	Code          string `json:"code"`
	ConfirmKey    string `json:"confirm_key"`
	Confirmation  string `json:"confirmation"`
	Confirmed     string `json:"confirmed"`
}

type checkVectors struct {
	Description string      `json:"description"`
	Cases       []checkCase `json:"cases"`
}

func TestCheckVectors(t *testing.T) {
	var cv checkVectors
	readVectors(t, "check.json", &cv)
	if len(cv.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cv.Cases {
		ka, kw, pub := unb64(t, c.AskerKey), unb64(t, c.AnswerKey), unb64(t, c.PublicKey)
		if CheckPublicKey(ka) != nil || CheckPublicKey(kw) != nil || CheckPublicKey(pub) != nil {
			t.Errorf("%s: not a key", c.Name)
		}
		if b64.EncodeToString(Commitment(ka)) != c.Commitment {
			t.Errorf("%s: commitment", c.Name)
		}
		if got := CheckCode(ka, kw, pub); got != c.Code {
			t.Errorf("%s: code %s, not %s", c.Name, got, c.Code)
		}
		asking, err := ConfirmKey(unb64(t, c.AskerPrivate), kw, ka, kw, pub)
		if err != nil || b64.EncodeToString(asking) != c.ConfirmKey {
			t.Errorf("%s: the asking side's confirm key: %v", c.Name, err)
		}
		waiting, err := ConfirmKey(unb64(t, c.AnswerPrivate), ka, ka, kw, pub)
		if err != nil || b64.EncodeToString(waiting) != c.ConfirmKey {
			t.Errorf("%s: the waiting side's confirm key: %v", c.Name, err)
		}
		if got, err := Unlock(waiting, CheckContext(c.Check), unb64(t, c.Confirmation)); err != nil || string(got) != c.Confirmed {
			t.Errorf("%s: confirmation %q, %v", c.Name, got, err)
		}
	}
}

// signCase is a signature of contract/crypto/sign.json, with the message it signs.
type signCase struct {
	Name       string  `json:"name"`
	Folder     string  `json:"folder,omitempty"`
	Version    *int    `json:"version,omitempty"`
	FolderKey  string  `json:"folder_key,omitempty"`
	FolderName *string `json:"folder_name,omitempty"`
	NewRoot    string  `json:"new_root,omitempty"`
	Message    string  `json:"message"`
	Signature  string  `json:"signature"`
}

type signVectors struct {
	Description string     `json:"description"`
	PrivateKey  string     `json:"private_key"`
	PublicKey   string     `json:"public_key"`
	Fingerprint string     `json:"fingerprint"`
	Verify      []signCase `json:"verify"`
	Refuse      []signCase `json:"refuse"`
}

// message is the message a case says it signs, made from its parts.
func (c signCase) message(t *testing.T) []byte {
	t.Helper()
	switch {
	case c.FolderKey != "":
		return FolderKeyMessage(c.Folder, *c.Version, unb64(t, c.FolderKey))
	case c.FolderName != nil:
		return PlainMessage(c.Folder, *c.Version, *c.FolderName)
	case c.NewRoot != "":
		return RootMessage(unb64(t, c.NewRoot))
	}
	t.Fatalf("%s: no message", c.Name)
	return nil
}

func TestSignVectors(t *testing.T) {
	var sv signVectors
	readVectors(t, "sign.json", &sv)
	public := unb64(t, sv.PublicKey)
	if pub, _ := PublicKey(unb64(t, sv.PrivateKey)); !bytes.Equal(pub, public) {
		t.Error("public key")
	}
	if b64.EncodeToString(Fingerprint(public)) != sv.Fingerprint {
		t.Error("fingerprint")
	}
	if len(sv.Verify) == 0 || len(sv.Refuse) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range sv.Verify {
		m := c.message(t)
		if b64.EncodeToString(m) != c.Message {
			t.Errorf("%s: message", c.Name)
		}
		if !Verify(public, m, unb64(t, c.Signature)) {
			t.Errorf("%s: doesn't verify", c.Name)
		}
	}
	for _, c := range sv.Refuse {
		if Verify(public, unb64(t, c.Message), unb64(t, c.Signature)) {
			t.Errorf("%s verifies", c.Name)
		}
	}
}

func TestSign(t *testing.T) {
	priv, pub := GenerateKey()
	m := PlainMessage("0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d", 0, "Família")
	sig, err := Sign(priv, m)
	if err != nil || len(sig) != SignatureSize || !Verify(pub, m, sig) {
		t.Fatalf("sign: %x, %v", sig, err)
	}
	other, otherPub := GenerateKey()
	otherSig, _ := Sign(other, m)
	flipped := bytes.Clone(sig)
	flipped[10] ^= 1
	for name, ok := range map[string]bool{
		"another key's signature": Verify(pub, m, otherSig),
		"another public key":      Verify(otherPub, m, sig),
		"another name":            Verify(pub, PlainMessage("0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d", 0, "Familia"), sig),
		"another version":         Verify(pub, PlainMessage("0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d", 1, "Família"), sig),
		"a flipped bit":           Verify(pub, m, flipped),
		"cut short":               Verify(pub, m, sig[:63]),
		"no key":                  Verify(pub[:64], m, sig),
	} {
		if ok {
			t.Errorf("%s verifies", name)
		}
	}
	// The version ends before the name: a name starting with a digit isn't another version's.
	if bytes.Equal(PlainMessage("f", 1, "2x"), PlainMessage("f", 12, "x")) {
		t.Error("version and name run together")
	}
	if _, err := Sign(pub, m); err == nil {
		t.Error("signs with a public key")
	}
}

func TestVectors(t *testing.T) {
	if *update {
		writeNewVectors(t)
	}

	var sv sealVectors
	readVectors(t, "seal.json", &sv)
	for _, c := range sv.Open {
		got, err := Open(unb64(t, c.PrivateKey), c.Purpose, []byte(c.AAD), unb64(t, c.Sealed))
		if err != nil || !bytes.Equal(got, unb64(t, c.Plaintext)) {
			t.Errorf("seal %s: %v", c.Name, err)
		}
		if pub, _ := PublicKey(unb64(t, c.PrivateKey)); b64.EncodeToString(pub) != c.PublicKey {
			t.Errorf("seal %s: public key", c.Name)
		}
	}
	for _, c := range sv.Refuse {
		if _, err := Open(unb64(t, c.PrivateKey), c.Purpose, []byte(c.AAD), unb64(t, c.Sealed)); err == nil {
			t.Errorf("seal %s opens", c.Name)
		}
	}

	var lv lockVectors
	readVectors(t, "lock.json", &lv)
	for _, c := range lv.Secrets {
		key := SecretKey(unb64(t, c.Secret), c.Purpose)
		if b64.EncodeToString(key) != c.Key {
			t.Errorf("lock %s: key", c.Name)
		}
		if got, err := Unlock(key, []byte(c.AAD), unb64(t, c.Locked)); err != nil || !bytes.Equal(got, unb64(t, c.Plaintext)) {
			t.Errorf("lock %s: %v", c.Name, err)
		}
	}
	for _, c := range lv.Passwords {
		if got, err := PasswordUnlock(c.Password, []byte(c.AAD), unb64(t, c.Locked)); err != nil || !bytes.Equal(got, unb64(t, c.Plaintext)) {
			t.Errorf("password lock: %v", err)
		}
	}
	if b64.EncodeToString(ThumbKey(unb64(t, lv.Thumb.FileKey))) != lv.Thumb.ThumbKey {
		t.Error("thumb key")
	}
	if got, err := OpenThumb(unb64(t, lv.Thumb.FileKey), unb64(t, lv.Thumb.Sealed)); err != nil || !bytes.Equal(got, unb64(t, lv.Thumb.Plaintext)) {
		t.Errorf("thumb: %v", err)
	}

	var cv contentVectors
	readVectors(t, "content.json", &cv)
	key := unb64(t, cv.FileKey)
	h, err := ParseHeader(unb64(t, cv.Header))
	if err != nil {
		t.Fatal(err)
	}
	if b64.EncodeToString(ContentKey(key, h)) != cv.ContentKey {
		t.Error("content key")
	}
	for _, c := range cv.Cases {
		enc := Encrypt(key, h, plaintext(int(c.Size)))
		sum := sha256.Sum256(enc)
		if int64(len(enc)) != c.EncryptedSize || EncryptedSize(c.Size) != c.EncryptedSize || hex.EncodeToString(sum[:]) != c.SHA256 {
			t.Errorf("content of %d bytes", c.Size)
		}
		if c.Encrypted != "" && c.Encrypted != b64.EncodeToString(enc) {
			t.Errorf("content of %d bytes: the bytes", c.Size)
		}
	}

	var rv recoveryVectors
	readVectors(t, "recovery.json", &rv)
	secret := unb64(t, rv.Secret)
	if FormatRecoveryCode(secret) != rv.Code {
		t.Error("recovery code")
	}
	for _, s := range append([]string{rv.Code}, rv.AlsoReads...) {
		if got, err := ParseRecoveryCode(s); err != nil || !bytes.Equal(got, secret) {
			t.Errorf("recovery code %q: %v", s, err)
		}
	}
	for _, s := range rv.Refuses {
		if _, err := ParseRecoveryCode(s); err == nil {
			t.Errorf("recovery code %q is read", s)
		}
	}
	if b64.EncodeToString(SecretKey(secret, PurposeRecovery)) != rv.Key {
		t.Error("recovery key")
	}
	got, err := Unlock(SecretKey(secret, PurposeRecovery), RecoveryContext, unb64(t, rv.Lock["locked"]))
	if err != nil || b64.EncodeToString(got) != rv.Lock["private_key"] {
		t.Errorf("recovery lock: %v", err)
	}
	if pub, _ := PublicKey(got); b64.EncodeToString(pub) != rv.Lock["public_key"] {
		t.Error("recovery public key")
	}
}

func writeNewVectors(t *testing.T) {
	enc := b64.EncodeToString
	folderPriv, folderPub := GenerateKey()
	personPriv, personPub := GenerateKey()
	devicePriv, devicePub := GenerateKey()
	fileKey := make([]byte, FileKeySize)
	rand.Read(fileKey)
	const folderID, userID = "w3dding5x2k7mbqz4bwdbyj6qs", "u7ld5x2k7mbqz4bwdbyj6qsqxa"
	sealed := func(priv, pub []byte, purpose string, aad, pt []byte) sealCase {
		s, err := Seal(pub, purpose, aad, pt)
		if err != nil {
			t.Fatal(err)
		}
		return sealCase{Purpose: purpose, AAD: string(aad), PrivateKey: enc(priv), PublicKey: enc(pub), Plaintext: enc(pt), Sealed: enc(s)}
	}
	file := sealed(folderPriv, folderPub, PurposeFile, FolderContext(folderID, 1), fileKey)
	file.Name = "a file key for version 1 of a folder's key"
	folder := sealed(personPriv, personPub, PurposeFolder, FolderContext(folderID, 1), folderPriv)
	folder.Name = "a folder's private key for a person"
	person := sealed(devicePriv, devicePub, PurposePerson, PersonContext(userID), personPriv)
	person.Name = "a person's private key for one of their browsers"
	rootPriv, rootPub := GenerateKey()
	root := sealed(personPriv, personPub, PurposeRoot, RootContext, rootPriv)
	root.Name = "the recovery key's private key for an admin's person key"
	otherFolder, otherPurpose, flipped := file, file, file
	otherFolder.Name, otherFolder.AAD = "the file key, opened as if for version 2", string(FolderContext(folderID, 2))
	otherPurpose.Name, otherPurpose.Purpose = "the file key, opened as a folder key", PurposeFolder
	fb := unb64(t, file.Sealed)
	fb[len(fb)-1] ^= 1
	flipped.Name, flipped.Sealed = "the file key with its last bit flipped", enc(fb)
	writeVectors(t, "seal.json", sealVectors{
		Description: "Keys Share seals with HPKE (contract/crypto/hpke_rfc9180.json has the suite): sealed is the encapsulated key (65 bytes), then the ciphertext; purpose is HPKE's info, aad its associated data, both as UTF-8. Every case in open opens with private_key to plaintext, whose public key is public_key; none in refuse opens. Binary values are base64url without padding.",
		Open:        []sealCase{file, folder, person, root},
		Refuse:      []sealCase{otherFolder, otherPurpose, flipped},
	})

	var secrets []secretCase
	note := []byte(`{"root":"` + enc(rootPub) + `","folders":{"` + folderID + `":1}}`)
	pinSecret := make([]byte, 32)
	rand.Read(pinSecret)
	for _, c := range []struct {
		name, purpose string
		secret        []byte // a new one without
		aad, pt       []byte
	}{
		{"a folder key, locked with an invite link's secret", PurposeInvite, nil, FolderContext(folderID, 1), folderPriv},
		{"a person key, locked with the secret of an invite for a new phone", PurposeInvite, nil, PersonContext(userID), personPriv},
		{"the root, locked with an invite link's secret", PurposeInvite, nil, RootContext, rootPub},
		{"a folder key, locked with a PIN link's secret", PurposePin, pinSecret, FolderContext(folderID, 1), folderPriv},
		{"the root, locked with a PIN link's secret", PurposePin, pinSecret, RootContext, rootPub},
		{"a PIN link's secret, locked with a key from the folder's private key", PurposePinSecret, folderPriv, FolderContext(folderID, 1), pinSecret},
		{"a person's note, locked with a key from their private key", PurposeNote, personPriv, NoteContext(userID), note},
	} {
		secret := c.secret
		if secret == nil {
			secret = make([]byte, 32)
			rand.Read(secret)
		}
		key := SecretKey(secret, c.purpose)
		secrets = append(secrets, secretCase{Name: c.name, Purpose: c.purpose, Secret: enc(secret), Key: enc(key), AAD: string(c.aad), Plaintext: enc(c.pt), Locked: enc(Lock(key, c.aad, c.pt))})
	}
	jpeg := []byte("\xff\xd8\xff\xe0 a thumbnail's JPEG bytes \xff\xd9")
	writeVectors(t, "lock.json", lockVectors{
		Description: "What Share locks with a key: a random nonce (12 bytes), then AES-256-GCM's ciphertext and tag, with aad (UTF-8) as associated data. A link's secret gives the key with HKDF-SHA256 (no salt, purpose as info, 32 bytes), and so does a private key's scalar: a folder key's for its PIN links' secrets, a person key's for their note (JSON: the root and the newest version of each folder's key their devices have seen). A password lock starts with a salt (16 bytes) and PBKDF2's iterations (4 bytes, big endian); PBKDF2-HMAC-SHA256 of the password's UTF-8 bytes gives the key. A thumbnail is locked with HKDF-SHA256 of the file key (no salt, share-e2ee-v1/thumb as info) and no aad. Binary values are base64url without padding.",
		Secrets:     secrets,
		Passwords: []passwordCase{
			{Password: "correct horse", AAD: string(PersonContext(userID)), Plaintext: enc(personPriv), Locked: enc(PasswordLock("correct horse", PersonContext(userID), personPriv))},
			{Password: "Grüße, Ünal ✓ 日本", AAD: string(PersonContext(userID)), Plaintext: enc(personPriv), Locked: enc(PasswordLock("Grüße, Ünal ✓ 日本", PersonContext(userID), personPriv))},
		},
		Thumb: thumbCase{FileKey: enc(fileKey), ThumbKey: enc(ThumbKey(fileKey)), Plaintext: enc(jpeg), Sealed: enc(SealThumb(fileKey, jpeg))},
	})

	h := NewHeader()
	var cases []contentCase
	for _, n := range []int64{0, 1, 100, ChunkSize, ChunkSize + 1, 200_000} {
		e := Encrypt(fileKey, h, plaintext(int(n)))
		sum := sha256.Sum256(e)
		c := contentCase{Size: n, EncryptedSize: int64(len(e)), SHA256: hex.EncodeToString(sum[:])}
		if n <= 100 {
			c.Encrypted = enc(e)
		}
		cases = append(cases, c)
	}
	writeVectors(t, "content.json", contentVectors{
		Description: "A file's contents, encrypted with file_key after header: \"SHE1\", the chunk size 65536 (4 bytes, big endian), a nonce prefix (7 bytes) and a zero byte. content_key is HKDF-SHA256 of the file key, salted with the header, with share-e2ee-v1/content as info. Chunk i is AES-256-GCM with content_key, no associated data, and the nonce prefix, i (4 bytes, big endian) and 1 for the last chunk, else 0. Every chunk holds 65536 bytes but the last, which holds the rest: 1 to 65536 bytes, or none in an empty file. A case's plaintext is size bytes, byte i being i mod 251; sha256 is that of all encrypted bytes, header included, and encrypted all of them for the small ones. Binary values are base64url without padding.",
		FileKey:     enc(fileKey),
		Header:      enc(h[:]),
		ContentKey:  enc(ContentKey(fileKey, h)),
		Cases:       cases,
	})

	secret, code := NewRecoveryCode()
	recoveryPriv, recoveryPub := GenerateKey()
	key := SecretKey(secret, PurposeRecovery)
	writeVectors(t, "recovery.json", recoveryVectors{
		Description: "The recovery code: 20 random bytes (secret), written as 32 characters of Crockford's base32 (0-9 and A-Z without I, L, O, U), most significant bits first, in groups of four joined by dashes. Reading one back takes small letters, O for 0, I and L for 1, and ignores dashes and spaces; it refuses anything else and other lengths. key is HKDF-SHA256 of the secret (no salt, share-e2ee-v1/recovery as info); it locks the recovery key's private key with the aad \"recovery\" (contract/crypto/lock.json has the format). Binary values are base64url without padding.",
		Secret:      enc(secret),
		Code:        code,
		AlsoReads:   []string{strings.ToLower(code), strings.ReplaceAll(code, "-", " "), strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(code, "-", ""), "0", "O"), "1", "l")},
		Refuses:     []string{code[:len(code)-2], code + "-0000", "U" + code[1:], code[:5] + "*" + code[6:]},
		Key:         enc(key),
		Lock:        map[string]string{"private_key": enc(recoveryPriv), "public_key": enc(recoveryPub), "locked": enc(Lock(key, RecoveryContext, recoveryPriv))},
	})
	writeSignVectors(t, rootPriv, rootPub, folderID, folderPub)
	writeCheckVectors(t, devicePub, personPub)
	_ = ecdh.P256
}

func writeSignVectors(t *testing.T, rootPriv, rootPub []byte, folderID string, folderPub []byte) {
	enc := b64.EncodeToString
	signed := func(name string, c signCase) signCase {
		c.Name = name
		m := c.message(t)
		sig, err := Sign(rootPriv, m)
		if err != nil {
			t.Fatal(err)
		}
		c.Message, c.Signature = enc(m), enc(sig)
		return c
	}
	v := func(n int) *int { return &n }
	s := func(n string) *string { return &n }
	_, newRoot := GenerateKey()
	key := signed("version 1 of a folder's key", signCase{Folder: folderID, Version: v(1), FolderKey: enc(folderPub)})
	plain := signed("a folder that was never encrypted, under a name starting with a digit", signCase{Folder: folderID, Version: v(0), FolderName: s("2026 Ürlaub ✓")})
	off := signed("a folder switched off after version 3", signCase{Folder: folderID, Version: v(3), FolderName: s("Family")})
	next := signed("the key of a new recovery code, signed with the old one", signCase{NewRoot: enc(newRoot)})
	refuse := func(name string, c signCase, m []byte) signCase {
		return signCase{Name: name, Message: enc(m), Signature: c.Signature}
	}
	flipped := unb64(t, key.Signature)
	flipped[40] ^= 1
	writeVectors(t, "sign.json", signVectors{
		Description: "What the root key (the recovery key) signs: ECDSA on P-256 with SHA-256 of the message, the signature as r and s, 32 bytes each, big endian. A version of a folder's key: \"share-e2ee-v1/sign/folder-key\", then \"folder:<folder>:<version>\", then its public key (65 bytes). A folder that sends plain: \"share-e2ee-v1/sign/plain\", then \"folder:<folder>:<its newest version, 0 if none>\", a newline and its name (UTF-8). The key of a new recovery code: \"share-e2ee-v1/sign/root\", then its public key, signed with the old one. Every case in verify verifies with public_key, and its message is made from its parts as above; none in refuse verifies. fingerprint is the first 16 bytes of SHA-256 of the public key, which a PIN's link carries. ECDSA signs with a random nonce: signing again gives other bytes that verify too. Binary values are base64url without padding.",
		PrivateKey:  enc(rootPriv),
		PublicKey:   enc(rootPub),
		Fingerprint: enc(Fingerprint(rootPub)),
		Verify:      []signCase{key, plain, off, next},
		Refuse: []signCase{
			refuse("the folder key's signature, for version 2", key, FolderKeyMessage(folderID, 2, folderPub)),
			refuse("the plain statement, for another name", off, PlainMessage(folderID, 3, "Family ")),
			refuse("the plain statement, for a newer version", off, PlainMessage(folderID, 4, "Family")),
			refuse("the folder key's signature, as a plain statement", key, PlainMessage(folderID, 1, "")),
			{Name: "the folder key's signature with a flipped bit", Message: key.Message, Signature: enc(flipped)},
		},
	})
}

func writeCheckVectors(t *testing.T, devicePub, personPub []byte) {
	enc := b64.EncodeToString
	const checkID = "0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d"
	askerPriv, askerKey := GenerateKey()
	answerPriv, answerKey := GenerateKey()
	otherPriv, otherKey := GenerateKey()
	_, rootPub := GenerateKey()
	made := func(name string, answerPriv, answerKey, public []byte, confirmed string) checkCase {
		k, err := ConfirmKey(askerPriv, answerKey, askerKey, answerKey, public)
		if err != nil {
			t.Fatal(err)
		}
		return checkCase{
			Name: name, Check: checkID,
			AskerPrivate: enc(askerPriv), AskerKey: enc(askerKey), AnswerPrivate: enc(answerPriv), AnswerKey: enc(answerKey), PublicKey: enc(public),
			Commitment: enc(Commitment(askerKey)), Code: CheckCode(askerKey, answerKey, public),
			ConfirmKey: enc(k), Confirmation: enc(Lock(k, CheckContext(checkID), []byte(confirmed))), Confirmed: confirmed,
		}
	}
	personPriv, personPub2 := GenerateKey()
	device := `{"root":"` + enc(rootPub) + `","person":{"public_key":"` + enc(personPub2) + `","private_key":"` + enc(personPriv) + `"}}`
	person := `{"root":"` + enc(rootPub) + `"}`
	writeVectors(t, "check.json", checkVectors{
		Description: "A check, before a device passes keys on (docs/e2ee-plan.md). Each side has a one-time P-256 key pair. The asking device sends commitment = SHA-256(\"share-e2ee-v1/check\" || asker_key); the other side answers with answer_key; then the asking device reveals asker_key, for the answer it saw, which the other side checks against the commitment it saw before answering. Each side keeps what it saw then: the asking device makes its code from the answer it revealed for, so keys relayed later change neither code. Both show code: the first 4 bytes of SHA-256(\"share-e2ee-v1/code\" || asker_key || answer_key || public_key), read as a big-endian number, modulo 1000000, in 6 digits with leading zeros. public_key is the key that gets the keys: the waiting device's key, or the waiting person's key (65 bytes, uncompressed). After Allow, the asking device locks confirmed (UTF-8 JSON: the root, and for a device of its person the person's key pair) with confirm_key and the aad \"check:<check>\" (contract/crypto/lock.json has the format): HKDF-SHA256 of the ECDH secret (the shared point's x coordinate) of one side's one-time private key and the other side's one-time public key, salted with SHA-256(asker_key || answer_key || public_key), with \"share-e2ee-v1/confirm\" as info. Both sides make the same confirm_key. Binary values are base64url without padding.",
		Cases: []checkCase{
			made("a new device's key", answerPriv, answerKey, devicePub, device),
			made("another key, same one-time keys", answerPriv, answerKey, personPub, person),
			made("another answer", otherPriv, otherKey, devicePub, device),
		},
	})
}
