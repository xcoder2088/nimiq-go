package nimiq

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidAddress(t *testing.T) {
	valid := []string{
		"NQ07 0000 0000 0000 0000 0000 0000 0000 0000", // the burn address (valid, but never ours)
		"NQ77 0000 0000 0000 0000 0000 0000 0000 0001", // the staking contract
		"NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXY", // taken from a real mainnet transaction
		"NQ30 7D50 N0UU G653 AQDC F1DS P1TU GBV6 V246",
		"nq08 act8 t0fe ptg8 p5rl h2s3 qgxh v15r nvxy", // case and spaces don't matter
		"NQ08ACT8T0FEPTG8P5RLH2S3QGXHV15RNVXY",
	}
	for _, a := range valid {
		if !ValidAddress(a) {
			t.Errorf("%q should be a valid address", a)
		}
	}
	invalid := []string{
		"", "NQ", "not an address",
		"NQ09 0000 0000 0000 0000 0000 0000 0000 0000", // wrong check digits
		"NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXZ", // one character changed
		"NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVX",  // too short
		"NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXYY",
		"NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXO", // 'O' isn't in the alphabet
		"XX08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXY",
	}
	for _, a := range invalid {
		if ValidAddress(a) {
			t.Errorf("%q must not be accepted", a)
		}
	}
	if !IsBurnAddress("nq07 0000 0000 0000 0000 0000 0000 0000 0000") || IsBurnAddress("NQ77 0000 0000 0000 0000 0000 0000 0000 0001") {
		t.Error("only the burn address is the burn address")
	}
}

func TestPaymentMatching(t *testing.T) {
	const addr = "NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXY"
	order := "abc123def456abc123def456" // a purchase id
	orderHex := hex.EncodeToString([]byte(order))
	good := Transaction{To: addr, Value: 1000, SenderData: orderHex, Confirmations: 5, ExecutionResult: true}

	if !paymentOK(good, "NQ08ACT8T0FEPTG8P5RLH2S3QGXHV15RNVXY", 1000, order) {
		t.Error("a right payment (message as hex, as real nodes send it) must be accepted")
	}
	plain := good
	plain.SenderData = order
	if !paymentOK(plain, addr, 1000, order) {
		t.Error("a message sent as plain text must also be accepted")
	}
	for name, mutate := range map[string]func(*Transaction){
		"too little":        func(x *Transaction) { x.Value = 999 },
		"another recipient": func(x *Transaction) { x.To = "NQ77 0000 0000 0000 0000 0000 0000 0000 0001" },
		"another order":     func(x *Transaction) { x.SenderData = hex.EncodeToString([]byte("someone-elses-order-id!!")) },
		"no message":        func(x *Transaction) { x.SenderData = "" },
		"failed execution":  func(x *Transaction) { x.ExecutionResult = false },
		"not deep enough":   func(x *Transaction) { x.Confirmations = MinConfirmations - 1 },
		"unconfirmed":       func(x *Transaction) { x.Confirmations = 0 },
	} {
		tx := good
		mutate(&tx)
		if paymentOK(tx, addr, 1000, order) {
			t.Errorf("%s: must not count as the payment", name)
		}
	}
	if !paymentOK(Transaction{To: addr, Value: 2000, Confirmations: 3, ExecutionResult: true}, addr, 1000, "") {
		t.Error("with no order reference to match, a big-enough payment counts")
	}
}

func fakeNode(t *testing.T, handler func(method string, params []interface{}) interface{}) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		out := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": map[string]interface{}{"data": handler(req.Method, req.Params), "metadata": nil}}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL)
}

func TestVerifyPaymentByHashAndNetwork(t *testing.T) {
	const addr = "NQ08 ACT8 T0FE PTG8 P5RL H2S3 QGXH V15R NVXY"
	order := "purchase-reference-01"
	hash := strings.Repeat("ab", 32)
	c := fakeNode(t, func(m string, p []interface{}) interface{} {
		switch m {
		case "getTransactionByHash":
			return map[string]interface{}{"hash": hash, "to": addr, "value": 5000, "senderData": hex.EncodeToString([]byte(order)),
				"confirmations": 9, "executionResult": true, "blockNumber": 1}
		case "getBlockNumber":
			return 62669271
		case "getBlockByNumber":
			return map[string]interface{}{"network": "MainAlbatross"}
		}
		return nil
	})
	if ok, err := c.VerifyPayment(hash, addr, 5000, hex.EncodeToString([]byte(order))); err != nil || !ok {
		t.Errorf("the payment should be verified by its hash: %v %v", ok, err)
	}
	if ok, _ := c.VerifyPayment(hash, addr, 5001, hex.EncodeToString([]byte(order))); ok {
		t.Error("an amount that is too small must not verify")
	}
	if _, err := c.VerifyPayment("not-a-hash", addr, 1, ""); err == nil {
		t.Error("garbage that isn't a transaction hash must be refused before asking the node")
	}
	if n, err := c.Network(); err != nil || n != "MainAlbatross" {
		t.Errorf("network: %q %v", n, err)
	}
}
