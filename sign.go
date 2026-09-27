package nimiq

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// signedMessagePrefix is what a Nimiq wallet puts in front of any text it is asked to sign, so a
// signature made for a website can never double as a signature on a transaction.
const signedMessagePrefix = "\x16Nimiq Signed Message:\n"

const addressAlphabet = "0123456789ABCDEFGHJKLMNPQRSTUVXY"

// AddressFromPublicKey is the "NQxx XXXX …" address that belongs to an Ed25519 public key: the first
// 20 bytes of its Blake2b-256 hash, written in Nimiq's base32 with an IBAN-style check.
func AddressFromPublicKey(pub []byte) (string, error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", errors.New("nimiq: a public key is 32 bytes")
	}
	sum := blake2b.Sum256(pub)
	var bits big.Int
	bits.SetBytes(sum[:20])
	base32 := make([]byte, 32)
	mask := big.NewInt(31)
	for i := 31; i >= 0; i-- {
		var d big.Int
		d.And(&bits, mask)
		base32[i] = addressAlphabet[d.Int64()]
		bits.Rsh(&bits, 5)
	}
	check := ibanCheck(string(base32) + "NQ00")
	s := "NQ" + fmt.Sprintf("%02d", 98-check) + string(base32)
	var parts []string
	for i := 0; i < len(s); i += 4 {
		parts = append(parts, s[i:i+4])
	}
	return strings.Join(parts, " "), nil
}

// ibanCheck is the number modulo 97 of a string whose letters count as 10…35.
func ibanCheck(s string) int {
	var digits strings.Builder
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			digits.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			digits.WriteString(strconv.Itoa(int(c) - 55))
		}
	}
	n, _ := new(big.Int).SetString(digits.String(), 10)
	return int(new(big.Int).Mod(n, big.NewInt(97)).Int64())
}

// VerifySignedMessage says whether signature is a valid signature, by the owner of address, of
// message as a Nimiq wallet signs it (SHA-256 of the prefix, the message's length and the message).
// The public key travels with the signature; it only counts if it really hashes to that address.
func VerifySignedMessage(address string, pub, signature []byte, message string) bool {
	if !ValidAddress(address) || IsBurnAddress(address) || len(pub) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return false
	}
	derived, err := AddressFromPublicKey(pub)
	if err != nil || !addressEqual(derived, address) {
		return false
	}
	digest := sha256.Sum256([]byte(signedMessagePrefix + strconv.Itoa(len(message)) + message))
	return ed25519.Verify(pub, digest[:], signature)
}

// SignMessageForTest signs the way a wallet does; the site itself never holds anybody's key.
func SignMessageForTest(priv ed25519.PrivateKey, message string) []byte {
	digest := sha256.Sum256([]byte(signedMessagePrefix + strconv.Itoa(len(message)) + message))
	return ed25519.Sign(priv, digest[:])
}
