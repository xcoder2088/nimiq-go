package nimiq

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestSignedMessageProvesOwnershipOfAnAddress(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	addr, err := AddressFromPublicKey(pub)
	if err != nil || !ValidAddress(addr) {
		t.Fatalf("derived address %q is not valid: %v", addr, err)
	}
	msg := "PixelDrop sign-in\nNonce: abc"
	sig := SignMessageForTest(priv, msg)
	if !VerifySignedMessage(addr, pub, sig, msg) {
		t.Fatal("a good signature was refused")
	}
	if VerifySignedMessage(addr, pub, sig, msg+"x") {
		t.Fatal("a signature on another message was accepted")
	}
	otherPub, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	if VerifySignedMessage(addr, otherPub, SignMessageForTest(otherPriv, msg), msg) {
		t.Fatal("someone else's key was accepted for this address")
	}
	if VerifySignedMessage(addr, pub, sig[:63], msg) || VerifySignedMessage("", pub, sig, msg) || VerifySignedMessage(BurnAddress, pub, sig, msg) {
		t.Fatal("malformed input must be refused")
	}
}

// Only a real wallet can confirm that the derivation matches Nimiq's own (that is checked live with a
// real sign-in); this at least guarantees the output is always a well-formed address.
func TestDerivedAddressIsAlwaysWellFormed(t *testing.T) {
	pub := make([]byte, 32) // all-zero key
	got, err := AddressFromPublicKey(pub)
	if err != nil || !ValidAddress(got) {
		t.Fatalf("%q %v", got, err)
	}
}
