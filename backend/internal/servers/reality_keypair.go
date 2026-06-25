package servers

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
)

func GenerateRealityKeypair() (RealityKeypair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return RealityKeypair{}, err
	}

	return RealityKeypair{
		PrivateKey: encodeRealityKey(privateKey.Bytes()),
		PublicKey:  encodeRealityKey(privateKey.PublicKey().Bytes()),
	}, nil
}

func encodeRealityKey(key []byte) string {
	return base64.RawURLEncoding.EncodeToString(key)
}
