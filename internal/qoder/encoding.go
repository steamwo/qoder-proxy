package qoder

import "encoding/base64"

const (
	qoderStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	qoderCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
)

var qoderS2C = func() [128]int16 {
	var table [128]int16
	for i := range table {
		table[i] = -1
	}
	for i := 0; i < 64; i++ {
		table[qoderStdAlphabet[i]] = int16(qoderCustomAlphabet[i])
	}
	table['='] = int16('$')
	return table
}()

// qoderEncodeBody applies Qoder's body obfuscation used with &Encode=1.
// The encoded bytes, not the plaintext JSON, must be used for COSY signing.
func qoderEncodeBody(plaintext []byte) []byte {
	std := base64.StdEncoding.EncodeToString(plaintext)
	n := len(std)
	a := n / 3
	rearranged := std[n-a:] + std[a:n-a] + std[:a]

	out := make([]byte, n)
	for i := 0; i < n; i++ {
		c := rearranged[i]
		if c < 128 && qoderS2C[c] >= 0 {
			out[i] = byte(qoderS2C[c])
		} else {
			out[i] = c
		}
	}
	return out
}
