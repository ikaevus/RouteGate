package wireguard

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const (
	DefaultServerAddress = "10.66.0.1/24"
	DefaultDNSAddress    = "1.1.1.1"
	DefaultListenPort    = 51820
)

type Keypair struct {
	PrivateKey string
	PublicKey  string
}

func GenerateKeypair() (Keypair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Keypair{}, fmt.Errorf("generate WireGuard private key: %w", err)
	}
	return Keypair{
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey.Bytes()),
		PublicKey:  base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
	}, nil
}

func PublicKeyFromPrivate(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != 32 {
		return "", errors.New("WireGuard private key must be a base64-encoded 32-byte value")
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(decoded)
	if err != nil {
		return "", fmt.Errorf("load WireGuard private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

func ValidateKey(value string) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != 32 {
		return errors.New("WireGuard keys must be base64-encoded 32-byte values")
	}
	return nil
}

func NextPeerAddress(serverAddress string, used []string) (string, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(serverAddress))
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return "", errors.New("WireGuard server address must be an IPv4 prefix with at least two peer addresses")
	}
	serverIP := prefix.Addr().Unmap()
	prefix = prefix.Masked()
	usedAddresses := make(map[netip.Addr]struct{}, len(used)+2)
	usedAddresses[prefix.Addr()] = struct{}{}
	usedAddresses[serverIP] = struct{}{}
	for _, value := range used {
		address, parseErr := netip.ParseAddr(strings.TrimSpace(value))
		if parseErr != nil {
			if prefix, prefixErr := netip.ParsePrefix(strings.TrimSpace(value)); prefixErr == nil {
				address, parseErr = prefix.Addr(), nil
			}
		}
		if parseErr == nil {
			usedAddresses[address.Unmap()] = struct{}{}
		}
	}

	candidate := prefix.Addr().Next()
	last := lastAddress(prefix)
	for candidate.IsValid() && candidate.Compare(last) < 0 {
		if _, exists := usedAddresses[candidate]; !exists {
			return candidate.String(), nil
		}
		candidate = candidate.Next()
	}
	return "", errors.New("WireGuard address pool is exhausted")
}

func PeerAddressInServerPrefix(serverAddress, peerAddress string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(serverAddress))
	if err != nil || !prefix.Addr().Is4() {
		return false
	}
	serverIP := prefix.Addr().Unmap()
	address, err := netip.ParseAddr(strings.TrimSpace(peerAddress))
	if err != nil {
		if peerPrefix, prefixErr := netip.ParsePrefix(strings.TrimSpace(peerAddress)); prefixErr == nil {
			address, err = peerPrefix.Addr(), nil
		}
	}
	if err != nil {
		return false
	}
	address = address.Unmap()
	prefix = prefix.Masked()
	return prefix.Contains(address) &&
		address != prefix.Addr().Unmap() &&
		address != lastAddress(prefix) &&
		address != serverIP
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	address := prefix.Masked().Addr().As4()
	hostBits := 32 - prefix.Bits()
	mask := uint32(1<<hostBits) - 1
	value := uint32(address[0])<<24 | uint32(address[1])<<16 | uint32(address[2])<<8 | uint32(address[3])
	value |= mask
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
