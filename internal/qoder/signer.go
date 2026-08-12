package qoder

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" // #nosec G501 -- required by Qoder's legacy COSY protocol.
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

const qoderModulusHex = "c0f22307e5cd362e296bb04470f6de8fbf935ce24e8fcf511a0e2701329769c4a76e499bb938036a52af1eaf818cf79a2600620e3ce87e371d2ca6d85803606a1b3fa5e874643c9ed2db7e85673ef7227fca56e2e7c08f0927609bb896a9f24be1782099a66016a5bfdc3f1ff756bfc9e88d7b5dc5be30bf45a0223a00ebcecf"

func md5Hex(data []byte) string {
	sum := md5.Sum(data) // #nosec G401 -- protocol compatibility, not password hashing.
	return hex.EncodeToString(sum[:])
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func aesCBCEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain = pkcs7Pad(plain, aes.BlockSize)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key).CryptBlocks(out, plain)
	return out, nil
}

func qoderPublicKey() (*rsa.PublicKey, error) {
	n := new(big.Int)
	if _, ok := n.SetString(qoderModulusHex, 16); !ok {
		return nil, fmt.Errorf("invalid qoder RSA modulus")
	}
	return &rsa.PublicKey{N: n, E: 65537}, nil
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func BuildHeaders(body []byte, requestURL string, cred credential.Credential) (http.Header, error) {
	if cred.UserID == "" || cred.Token == "" {
		return nil, fmt.Errorf("qoder credential requires user_id and token")
	}
	uuid, err := randomUUID()
	if err != nil {
		return nil, err
	}
	aesKey := []byte(uuid[:16])
	userInfo, err := json.Marshal(map[string]string{
		"uid": cred.UserID, "security_oauth_token": cred.Token, "name": cred.Name, "aid": "", "email": cred.Email,
	})
	if err != nil {
		return nil, err
	}
	encInfo, err := aesCBCEncrypt(userInfo, aesKey)
	if err != nil {
		return nil, err
	}
	pub, err := qoderPublicKey()
	if err != nil {
		return nil, err
	}
	encKey, err := rsa.EncryptPKCS1v15(rand.Reader, pub, aesKey) // #nosec G402 -- Qoder protocol requires PKCS#1 v1.5.
	if err != nil {
		return nil, err
	}
	requestID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	payloadJSON, err := json.Marshal(map[string]string{
		"version": "v1", "requestId": requestID,
		"info":        base64.StdEncoding.EncodeToString(encInfo),
		"cosyVersion": ClientVersion, "ideVersion": "",
	})
	if err != nil {
		return nil, err
	}
	payload := base64.StdEncoding.EncodeToString(payloadJSON)
	cosyKey := base64.StdEncoding.EncodeToString(encKey)
	u, err := url.Parse(requestURL)
	if err != nil {
		return nil, err
	}
	sigPath := u.Path
	if strings.HasPrefix(sigPath, "/algo") {
		sigPath = strings.TrimPrefix(sigPath, "/algo")
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sigInput := []byte(payload + "\n" + cosyKey + "\n" + timestamp + "\n" + string(body) + "\n" + sigPath)
	machineID := cred.MachineID
	if machineID == "" {
		machineID, err = randomUUID()
		if err != nil {
			return nil, err
		}
	}
	xRequestID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	h := make(http.Header)
	h.Set("Authorization", "Bearer COSY."+payload+"."+md5Hex(sigInput))
	h.Set("Cosy-Key", cosyKey)
	h.Set("Cosy-User", cred.UserID)
	h.Set("Cosy-Date", timestamp)
	h.Set("Cosy-Version", ClientVersion)
	h.Set("Cosy-Machineid", machineID)
	h.Set("Cosy-Machinetoken", machineID)
	h.Set("Cosy-Machinetype", "5")
	h.Set("Cosy-Machineos", "x86_64_windows")
	h.Set("Cosy-Clienttype", "5")
	h.Set("Cosy-Clientip", "127.0.0.1")
	h.Set("Cosy-Bodyhash", md5Hex(body))
	h.Set("Cosy-Bodylength", fmt.Sprintf("%d", len(body)))
	h.Set("Cosy-Sigpath", sigPath)
	h.Set("Cosy-Data-Policy", "disagree")
	h.Set("Cosy-Organization-Id", "")
	h.Set("Cosy-Organization-Tags", "")
	h.Set("Login-Version", "v2")
	h.Set("X-Request-Id", xRequestID)
	return h, nil
}
