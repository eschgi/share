package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HomeProof answers a phone's question whether the server at its address at home is its own:
// an HMAC of the phone's nonce, keyed with the hash of the phone's key, which only this server
// keeps. The phone works out the same from its key before it sends that key over plain http.
// contract/api/home_proof.json has the details and a worked example.
func HomeProof(tokenHash []byte, nonce string) string {
	mac := hmac.New(sha256.New, tokenHash)
	mac.Write([]byte("share-home-proof:" + nonce))
	return hex.EncodeToString(mac.Sum(nil))
}
