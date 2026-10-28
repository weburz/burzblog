package snapshot

import (
	"bufio"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

// An encrypted file (a snapshot, or one upload stored beside remote
// snapshots) is the plain file sealed with a key derived from a passphrase:
//
//	magic | log2(N) r p | salt (16) | check (16) | file nonce (16) | chunk | ...
//
// scrypt stretches the passphrase with a random salt into a master key and a
// check value that tells a wrong passphrase apart from a damaged file. One
// run may seal many files under the same salt, deriving the slow master key
// once; each file then has its own key, HMAC-SHA256(master, file nonce) with
// a random file nonce, so no two files ever share a key. The content is cut
// into 64 KiB chunks, each sealed with ChaCha20-Poly1305 under a nonce made
// of the chunk's number and a flag on the last one, with the header as
// associated data (the STREAM construction). So changing, reordering or
// dropping a chunk, cutting the file short or editing the header all fail to
// decrypt.
const (
	encMagic  = "BURZBLOG ENCRYPTED SNAPSHOT 1\n"
	chunkSize = 64 << 10
	saltSize  = 16
	checkSize = 16
	nonceSize = 16
	// scrypt cost for new files: 32 MiB of memory, under a second on a
	// Raspberry Pi 4, and small enough for a 128 MB container.
	writeLogN = 15
	writeR    = 8
	writeP    = 1
	// maxLogN is the largest scrypt cost a snapshot may ask for: one step
	// above what this version writes, so a later one can raise it once.
	maxLogN = 16
	// MinPassphrase is the shortest passphrase accepted for a new snapshot.
	MinPassphrase = 12
)

// Errors callers can branch on.
var (
	ErrPassphrase = errors.New("the passphrase is wrong")
	ErrDamaged    = errors.New("the snapshot is damaged, cut short or was changed")
	// ErrNeedPassphrase means the snapshot is encrypted and no passphrase was given.
	ErrNeedPassphrase = errors.New("the snapshot is encrypted; give its passphrase")
)

func headerSize() int { return len(encMagic) + 3 + saltSize + checkSize + nonceSize }

// deriveKeys stretches passphrase into the content key and the check value.
func deriveKeys(passphrase string, salt []byte, logN, r, p int) (key, check []byte, err error) {
	out, err := scrypt.Key([]byte(passphrase), salt, 1<<logN, r, p, chacha20poly1305.KeySize+checkSize)
	if err != nil {
		return nil, nil, err
	}
	return out[:chacha20poly1305.KeySize], out[chacha20poly1305.KeySize:], nil
}

// sealKey is a master key for sealing files in one run: the salt and the
// check value go in each file's header, with a fresh file nonce per file.
type sealKey struct {
	prefix []byte // magic, cost, salt and check value
	master []byte
}

// newSealKey derives a master key from passphrase with a new random salt.
func newSealKey(passphrase string) (*sealKey, error) {
	if len(passphrase) < MinPassphrase {
		return nil, fmt.Errorf("the passphrase must be at least %d characters", MinPassphrase)
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	master, check, err := deriveKeys(passphrase, salt, writeLogN, writeR, writeP)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, 0, headerSize())
	prefix = append(prefix, encMagic...)
	prefix = append(prefix, writeLogN, writeR, writeP)
	prefix = append(prefix, salt...)
	prefix = append(prefix, check...)
	return &sealKey{prefix: prefix, master: master}, nil
}

// fileAEAD is the cipher for the file whose nonce is fileNonce.
func fileAEAD(master, fileNonce []byte) (cipher.AEAD, error) {
	return chacha20poly1305.New(hmacSum(master, fileNonce))
}

func hmacSum(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// encryptTo seals one file with its own salt; see sealWith.
func encryptTo(w io.Writer, passphrase string) (io.WriteCloser, error) {
	k, err := newSealKey(passphrase)
	if err != nil {
		return nil, err
	}
	return k.sealTo(w)
}

// sealTo returns a writer that seals what is written to it into w under a new
// file key. Close writes the last chunk; without it the file cannot be
// decrypted.
func (k *sealKey) sealTo(w io.Writer) (io.WriteCloser, error) {
	fileNonce := make([]byte, nonceSize)
	if _, err := rand.Read(fileNonce); err != nil {
		return nil, err
	}
	header := append(append(make([]byte, 0, headerSize()), k.prefix...), fileNonce...)
	aead, err := fileAEAD(k.master, fileNonce)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return &sealer{w: w, aead: aead, aad: header, buf: make([]byte, 0, chunkSize)}, nil
}

type sealer struct {
	w       io.Writer
	aead    cipher.AEAD
	aad     []byte
	buf     []byte
	counter uint64
	closed  bool
}

func chunkNonce(counter uint64, last bool) []byte {
	nonce := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint64(nonce[3:11], counter)
	if last {
		nonce[11] = 1
	}
	return nonce
}

// Write holds back a full chunk until more data follows, because only Close
// knows which chunk is the last.
func (s *sealer) Write(p []byte) (int, error) {
	if s.closed {
		return 0, errors.New("write to a closed snapshot")
	}
	n := 0
	for len(p) > 0 {
		if len(s.buf) == chunkSize {
			if err := s.flush(false); err != nil {
				return n, err
			}
		}
		k := copy(s.buf[len(s.buf):chunkSize], p)
		s.buf = s.buf[:len(s.buf)+k]
		p = p[k:]
		n += k
	}
	return n, nil
}

func (s *sealer) flush(last bool) error {
	out := s.aead.Seal(nil, chunkNonce(s.counter, last), s.buf, s.aad)
	s.counter++
	s.buf = s.buf[:0]
	_, err := s.w.Write(out)
	return err
}

// Close seals the last chunk, which may be empty. It does not close w.
func (s *sealer) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.flush(true)
}

// isEncrypted reports whether r starts like an encrypted snapshot.
func isEncrypted(r *bufio.Reader) bool {
	head, _ := r.Peek(len(encMagic))
	return string(head) == encMagic
}

// keyring opens encrypted files with one passphrase, deriving each salt's
// master key once however many files share it.
type keyring struct {
	passphrase string
	mu         sync.Mutex
	masters    map[string]derivedKey // by cost and salt
	derived    int                   // scrypt runs, for tests
}

type derivedKey struct{ master, check []byte }

func newKeyring(passphrase string) *keyring {
	return &keyring{passphrase: passphrase, masters: map[string]derivedKey{}}
}

// decryptFrom opens one file with passphrase; see keyring.open.
func decryptFrom(r *bufio.Reader, passphrase string) (io.Reader, error) {
	return newKeyring(passphrase).open(r)
}

// open reads the header from r, checks the passphrase against it and returns
// the plain content. Reads fail with ErrDamaged if anything was changed or
// cut off.
func (k *keyring) open(r *bufio.Reader) (io.Reader, error) {
	header := make([]byte, headerSize())
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, ErrDamaged
	}
	if string(header[:len(encMagic)]) != encMagic {
		return nil, ErrDamaged
	}
	params := header[len(encMagic):]
	logN, cr, cp := int(params[0]), int(params[1]), int(params[2])
	// scrypt needs 128·N·r bytes and p times the work: accept no more than
	// the 64 MiB and single pass of N = 2^16, r = 8, p = 1, so a crafted
	// header cannot exhaust a small server before the passphrase is checked.
	if logN < 10 || logN > maxLogN || cr != writeR || cp != writeP {
		return nil, fmt.Errorf("%w: unknown key settings", ErrDamaged)
	}
	salt := params[3 : 3+saltSize]
	check := params[3+saltSize : 3+saltSize+checkSize]
	fileNonce := params[3+saltSize+checkSize:]

	id := string(params[:3+saltSize])
	k.mu.Lock()
	dk, ok := k.masters[id]
	k.mu.Unlock()
	if !ok {
		master, want, err := deriveKeys(k.passphrase, salt, logN, cr, cp)
		if err != nil {
			return nil, err
		}
		dk = derivedKey{master: master, check: want}
		k.mu.Lock()
		k.masters[id], k.derived = dk, k.derived+1
		k.mu.Unlock()
	}
	if subtle.ConstantTimeCompare(dk.check, check) != 1 {
		return nil, ErrPassphrase
	}
	aead, err := fileAEAD(dk.master, fileNonce)
	if err != nil {
		return nil, err
	}
	return &opener{r: r, aead: aead, aad: header, chunk: make([]byte, chunkSize+aead.Overhead())}, nil
}

type opener struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	aad     []byte
	chunk   []byte
	plain   []byte
	counter uint64
	done    bool
}

func (o *opener) Read(p []byte) (int, error) {
	for len(o.plain) == 0 {
		if o.done {
			return 0, io.EOF
		}
		if err := o.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, o.plain)
	o.plain = o.plain[n:]
	return n, nil
}

// next opens the following chunk. A chunk is the last when the file ends
// right after it; a file that ends after a chunk not sealed as the last was
// cut short.
func (o *opener) next() error {
	n, err := io.ReadFull(o.r, o.chunk)
	last := false
	switch {
	case errors.Is(err, io.EOF):
		return ErrDamaged
	case errors.Is(err, io.ErrUnexpectedEOF):
		last = true
	case err != nil:
		return err
	default:
		if _, err := o.r.Peek(1); errors.Is(err, io.EOF) {
			last = true
		} else if err != nil {
			return err
		}
	}
	plain, err := o.aead.Open(o.chunk[:0], chunkNonce(o.counter, last), o.chunk[:n], o.aad)
	if err != nil {
		return ErrDamaged
	}
	o.counter++
	o.plain, o.done = plain, last
	return nil
}
