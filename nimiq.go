// Package nimiq talks to a Nimiq Albatross JSON-RPC node to broadcast
// transactions and look up incoming payments by address.
//
// Field names below were confirmed against the live public nodes at
// https://rpc.nimiqwatch.com (mainnet) and https://rpc.testnet.nimiqwatch.com
// (getBlockNumber, getBlockByNumber, getTransactionByHash, getTransactionsByAddress).
// Every result is wrapped as {"data":..,"metadata":..}.
// getTransactionsByAddress takes 3 positional params: [address, max, null]
// (the third is a "start after this tx hash" cursor). The message attached
// to a basic transaction shows up as the "senderData" field.
package nimiq

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	RPCURL     string
	httpClient *http.Client
}

func NewClient(rpcURL string) *Client {
	return &Client{RPCURL: rpcURL, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcEnvelope struct {
	Result *struct {
		Data     json.RawMessage `json:"data"`
		Metadata json.RawMessage `json:"metadata"`
	} `json:"result"`
	Error *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// call returns the unwrapped `result.data` payload for method(params).
func (c *Client) call(method string, params []interface{}) (json.RawMessage, error) {
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Post(c.RPCURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("nimiq rpc: request failed: %w", err)
	}
	defer resp.Body.Close()

	var env rpcEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("nimiq rpc: bad response: %w", err)
	}
	if env.Error != nil {
		return nil, fmt.Errorf("nimiq rpc: %s (code %d, data %s)", env.Error.Message, env.Error.Code, string(env.Error.Data))
	}
	if env.Result == nil {
		return nil, fmt.Errorf("nimiq rpc: empty result")
	}
	return env.Result.Data, nil
}

// SendRawTransaction broadcasts a hex-encoded signed transaction and returns its hash.
func (c *Client) SendRawTransaction(rawHex string) (string, error) {
	data, err := c.call("sendRawTransaction", []interface{}{rawHex})
	if err != nil {
		return "", err
	}
	var hash string
	if err := json.Unmarshal(data, &hash); err == nil && hash != "" {
		return hash, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		if h, ok := obj["hash"].(string); ok {
			return h, nil
		}
	}
	return "", fmt.Errorf("nimiq rpc: could not parse tx hash from: %s", string(data))
}

type Transaction struct {
	Hash            string `json:"hash"`
	BlockNumber     int64  `json:"blockNumber"`
	Confirmations   int64  `json:"confirmations"`
	From            string `json:"from"`
	To              string `json:"to"`
	Value           uint64 `json:"value"`
	Fee             uint64 `json:"fee"`
	SenderData      string `json:"senderData"`
	RecipientData   string `json:"recipientData"`
	ExecutionResult bool   `json:"executionResult"`
}

// MinConfirmations is how many blocks deep a payment must be before it counts. Blocks come about
// once a second and are final within seconds, so this is a couple of seconds of waiting.
const MinConfirmations = 2

// dataMatches reports whether a transaction's message is the order reference we asked for. Nodes
// return the message hex-encoded; older answers carried it as plain text, so both are accepted.
func dataMatches(senderData, wantText string) bool {
	if wantText == "" {
		return true
	}
	if senderData == wantText {
		return true
	}
	if raw, err := hex.DecodeString(senderData); err == nil && string(raw) == wantText {
		return true
	}
	return false
}

// paymentOK is the single test a transaction must pass to count as the payment we're waiting for.
func paymentOK(tx Transaction, address string, expectedLuna uint64, wantText string) bool {
	return tx.ExecutionResult &&
		tx.Confirmations >= MinConfirmations &&
		addressEqual(tx.To, address) &&
		tx.Value >= expectedLuna &&
		dataMatches(tx.SenderData, wantText)
}

func wantText(extraDataHex string) string {
	if decoded, err := hex.DecodeString(extraDataHex); err == nil {
		return string(decoded)
	}
	return ""
}

// GetTransaction fetches one transaction by its hash — a single quick lookup, unlike scanning an
// address's history.
func (c *Client) GetTransaction(hash string) (*Transaction, error) {
	if len(hash) != 64 {
		return nil, fmt.Errorf("nimiq rpc: not a transaction hash")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return nil, fmt.Errorf("nimiq rpc: not a transaction hash")
	}
	data, err := c.call("getTransactionByHash", []interface{}{hash})
	if err != nil {
		return nil, err
	}
	var tx Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return nil, fmt.Errorf("nimiq rpc: unexpected getTransactionByHash shape: %w", err)
	}
	return &tx, nil
}

// VerifyPayment checks that the transaction with this hash is the payment we expect: to our address,
// at least the right amount, carrying the order reference, executed and deep enough. A transaction
// the node doesn't know (yet) is simply "not found".
func (c *Client) VerifyPayment(hash, address string, expectedLuna uint64, extraDataHex string) (found bool, err error) {
	tx, err := c.GetTransaction(hash)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(strings.ToLower(err.Error()), "unknown") {
			return false, nil
		}
		return false, err
	}
	return paymentOK(*tx, address, expectedLuna, wantText(extraDataHex)), nil
}

// FindPayment scans recent transactions to `address` for one paying at least
// expectedLuna with senderData matching extraDataHex (hex-encoded order ref).
// Returns the matching transaction hash if found.
func (c *Client) FindPayment(address string, expectedLuna uint64, extraDataHex string) (txHash string, found bool, err error) {
	data, err := c.call("getTransactionsByAddress", []interface{}{address, 50, nil})
	if err != nil {
		return "", false, err
	}

	var txs []Transaction
	if err := json.Unmarshal(data, &txs); err != nil {
		return "", false, fmt.Errorf("nimiq rpc: unexpected getTransactionsByAddress shape: %w", err)
	}

	want := wantText(extraDataHex)
	for _, tx := range txs {
		if paymentOK(tx, address, expectedLuna, want) {
			return tx.Hash, true, nil
		}
	}
	return "", false, nil
}

// BlockHeight is the chain's current block number, needed to build a transaction's
// validityStartHeight (the wallet must sign against a recent height).
func (c *Client) BlockHeight() (int64, error) {
	data, err := c.call("getBlockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}
	var height int64
	if err := json.Unmarshal(data, &height); err != nil || height < 2 {
		return 0, fmt.Errorf("nimiq rpc: unexpected block number")
	}
	return height, nil
}

// Network asks the node which network it is on: "MainAlbatross" or "TestAlbatross".
func (c *Client) Network() (string, error) {
	num, err := c.call("getBlockNumber", []interface{}{})
	if err != nil {
		return "", err
	}
	var height int64
	if err := json.Unmarshal(num, &height); err != nil || height < 2 {
		return "", fmt.Errorf("nimiq rpc: unexpected block number")
	}
	blk, err := c.call("getBlockByNumber", []interface{}{height - 1, false})
	if err != nil {
		return "", err
	}
	var b struct {
		Network string `json:"network"`
	}
	if err := json.Unmarshal(blk, &b); err != nil || b.Network == "" {
		return "", fmt.Errorf("nimiq rpc: the node did not say which network it is on")
	}
	return b.Network, nil
}

// BurnAddress is the address every coin sent to is destroyed at: never a place to receive money.
const BurnAddress = "NQ07 0000 0000 0000 0000 0000 0000 0000 0000"

// ValidAddress checks the format and checksum of a Nimiq address ("NQxx XXXX XXXX …", 36 characters
// once the spaces are removed, with an IBAN-style check).
func ValidAddress(address string) bool {
	s := strings.ToUpper(strings.ReplaceAll(address, " ", ""))
	if len(s) != 36 || !strings.HasPrefix(s, "NQ") {
		return false
	}
	const alphabet = "0123456789ABCDEFGHJKLMNPQRSTUVXY"
	for _, c := range s[4:] {
		if !strings.ContainsRune(alphabet, c) {
			return false
		}
	}
	// IBAN rule: move the first four characters to the end, turn letters into numbers (A=10 …),
	// and the whole number modulo 97 must be 1.
	moved := s[4:] + s[:4]
	var digits strings.Builder
	for _, c := range moved {
		switch {
		case c >= '0' && c <= '9':
			digits.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			digits.WriteString(fmt.Sprint(int(c) - 55))
		default:
			return false
		}
	}
	num, ok := new(big.Int).SetString(digits.String(), 10)
	if !ok {
		return false
	}
	return new(big.Int).Mod(num, big.NewInt(97)).Int64() == 1
}

// IsBurnAddress reports whether the address is the destruction address.
func IsBurnAddress(address string) bool { return addressEqual(address, BurnAddress) }

func addressEqual(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, " ", ""), strings.ReplaceAll(b, " ", ""))
}
