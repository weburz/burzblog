package snapshot

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
	"time"
)

const testPassphrase = "correct horse battery staple"

func seal(t *testing.T, plain []byte, writes int) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := encryptTo(&out, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	// Write in uneven pieces so chunk edges fall inside writes.
	step := len(plain)/writes + 1
	for rest := plain; len(rest) > 0; {
		n := min(step, len(rest))
		if _, err := w.Write(rest[:n]); err != nil {
			t.Fatal(err)
		}
		rest = rest[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func unseal(sealed []byte, passphrase string) ([]byte, error) {
	r, err := decryptFrom(bufio.NewReader(bytes.NewReader(sealed)), passphrase)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func random(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncryptRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3 * chunkSize, 3*chunkSize + 17} {
		plain := random(t, n)
		sealed := seal(t, plain, 7)
		if !isEncrypted(bufio.NewReader(bytes.NewReader(sealed))) {
			t.Fatalf("%d bytes: not recognised as encrypted", n)
		}
		if bytes.Contains(sealed, plain[:min(n, 64)]) && n >= 16 {
			t.Fatalf("%d bytes: plain text visible in the sealed file", n)
		}
		got, err := unseal(sealed, testPassphrase)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d bytes: round trip = %d bytes, %v", n, len(got), err)
		}
	}
}

func TestEncryptSaltsEveryFile(t *testing.T) {
	plain := []byte("same content")
	if a, b := seal(t, plain, 1), seal(t, plain, 1); bytes.Equal(a, b) {
		t.Error("two encryptions of the same content are identical")
	}
}

func TestDecryptRefusesWrongPassphrase(t *testing.T) {
	sealed := seal(t, random(t, 1000), 1)
	if _, err := unseal(sealed, "a different passphrase"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
}

func TestDecryptDetectsTampering(t *testing.T) {
	plain := random(t, 3*chunkSize+100)
	sealed := seal(t, plain, 3)
	h := headerSize()
	full := chunkSize + 16
	cases := map[string]func([]byte) []byte{
		"flipped content byte": func(b []byte) []byte { b[h+full+5] ^= 1; return b },
		"flipped last byte":    func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		"cut mid-chunk":        func(b []byte) []byte { return b[:h+full+100] },
		"cut at a chunk edge":  func(b []byte) []byte { return b[:h+2*full] },
		"cut after the header": func(b []byte) []byte { return b[:h] },
		"dropped middle chunk": func(b []byte) []byte { return append(b[:h+full:h+full], b[h+2*full:]...) },
		"swapped chunks": func(b []byte) []byte {
			out := append([]byte{}, b[:h]...)
			out = append(out, b[h+full:h+2*full]...)
			out = append(out, b[h:h+full]...)
			return append(out, b[h+2*full:]...)
		},
		"appended bytes": func(b []byte) []byte { return append(b, 0, 1, 2) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := unseal(change(append([]byte{}, sealed...)), testPassphrase)
			if !errors.Is(err, ErrDamaged) {
				t.Errorf("err = %v, want ErrDamaged", err)
			}
		})
	}
}

func TestDecryptRefusesChangedHeader(t *testing.T) {
	sealed := seal(t, []byte("hello"), 1)
	salt := len(encMagic) + 3
	b := append([]byte{}, sealed...)
	b[salt] ^= 1 // another salt derives another key: the check fails
	if _, err := unseal(b, testPassphrase); !errors.Is(err, ErrPassphrase) {
		t.Errorf("changed salt: %v", err)
	}
	// Costs that would need more than 64 MiB or more than one pass are
	// refused before scrypt runs.
	for name, at := range map[string][2]int{
		"N = 2^30": {0, 30}, "N = 2^17": {0, 17}, "r = 16": {1, 16}, "p = 2": {2, 2}, "r = 0": {1, 0},
	} {
		b = append([]byte{}, sealed...)
		b[len(encMagic)+at[0]] = byte(at[1])
		start := time.Now()
		if _, err := unseal(b, testPassphrase); !errors.Is(err, ErrDamaged) {
			t.Errorf("%s: %v", name, err)
		}
		if time.Since(start) > 50*time.Millisecond {
			t.Errorf("%s: refusing took %v, as if scrypt ran", name, time.Since(start))
		}
	}
}

func TestEncryptNeedsALongPassphrase(t *testing.T) {
	if _, err := encryptTo(io.Discard, "short"); err == nil {
		t.Error("a 5-character passphrase was accepted")
	}
}

func sealWith(t *testing.T, k *sealKey, plain []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := k.sealTo(&out)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(plain)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestFilesSealedTogetherNeverShareAKey(t *testing.T) {
	k, err := newSealKey(testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("same bytes "), 10000)
	a, b := sealWith(t, k, plain), sealWith(t, k, plain)
	h := headerSize()
	if bytes.Equal(a[h:h+64], b[h:h+64]) {
		t.Fatal("two files sealed in one run produced the same ciphertext: a key and nonce were reused")
	}
	// Chunk 0 of one file must not open inside the other.
	swapped := append(append([]byte{}, a[:h]...), b[h:]...)
	if _, err := unseal(swapped, testPassphrase); !errors.Is(err, ErrDamaged) {
		t.Errorf("a chunk moved between files: %v", err)
	}
	// One passphrase derivation opens every file of the run.
	ring := newKeyring(testPassphrase)
	for _, f := range [][]byte{a, b, sealWith(t, k, nil)} {
		r, err := ring.open(bufio.NewReader(bytes.NewReader(f)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(r); err != nil {
			t.Fatal(err)
		}
	}
	if ring.derived != 1 {
		t.Errorf("scrypt ran %d times for three files of one run", ring.derived)
	}
	if _, err := newKeyring("not the passphrase").open(bufio.NewReader(bytes.NewReader(a))); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase through a keyring: %v", err)
	}
}

func TestDecryptRefusesChangedFileNonce(t *testing.T) {
	sealed := seal(t, []byte("hello"), 1)
	sealed[headerSize()-1] ^= 1
	if _, err := unseal(sealed, testPassphrase); !errors.Is(err, ErrDamaged) {
		t.Errorf("changed file nonce: %v", err)
	}
}
