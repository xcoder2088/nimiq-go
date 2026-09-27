# nimiq-go

A small, dependency-light Go package for talking to the [Nimiq](https://www.nimiq.com) blockchain:
verifying payments, checking and deriving addresses, and verifying wallet-signed messages — the kind
of thing a service needs to accept NIM payments or offer "sign in with your Nimiq wallet", without
ever holding a private key itself.

Built for [PixelDrop](https://pixeldrop.ca), a private file-storage service, and split out on its own
because none of it is specific to that app.

## What's in it

- **`Client`** — a JSON-RPC client for a public Nimiq Albatross node (confirmed against the real
  public nodes at `rpc.nimiqwatch.com` and `rpc.testnet.nimiqwatch.com`):
  - `VerifyPayment` / `FindPayment` — check that a specific payment (to an address, for an amount,
    carrying a reference in its data) has happened and is confirmed.
  - `SendRawTransaction` — broadcast a transaction a wallet has already signed. This package never
    signs anything itself; a wallet does that, and this only ever forwards what it produced.
  - `GetTransaction`, `BlockHeight`, `Network` — supporting lookups.
- **Address handling** — `ValidAddress` (format + IBAN-style checksum), `IsBurnAddress`,
  `AddressFromPublicKey` (deriving the "NQxx …" address a public key corresponds to).
- **Signed messages** — `VerifySignedMessage` checks that a signature over some text was really made
  by the wallet holding a given address, the way a Nimiq wallet signs an arbitrary message (used for
  passwordless "sign in with your wallet" flows). No private key ever touches this code; it only
  verifies what a wallet already signed client-side.

## Design notes

- No Nimiq SDK dependency — RPC calls, address derivation and signature verification are all
  implemented directly against the standard library (plus `golang.org/x/crypto/blake2b` for the
  address hash), so there's nothing here pulling in a large or unmaintained third-party client.
- Never holds or asks for a private key. Every operation either verifies something a wallet already
  signed, or broadcasts an already-signed transaction. Building the actual application logic around
  it (an account model, a UI, rate limits, what "confirmed" should trigger) is deliberately left to
  the caller.

## Status

Extracted from PixelDrop's own backend, where it's used in production on Nimiq mainnet. Not yet
published as its own module — this repository is the local, standalone form of it.
