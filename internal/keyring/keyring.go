package keyring

import (
	"fmt"
	"sync"

	gokeyring "github.com/zalando/go-keyring"
)

const serviceName = "doit-sync"

var (
	initOnce         sync.Once
	mu               sync.Mutex
	keyringAvailable = false
	testMode         = false
)

func ensureInitialized() {
	initOnce.Do(func() {
		mu.Lock()
		defer mu.Unlock()

		if testMode {
			keyringAvailable = false
			return
		}

		if err := gokeyring.Set(serviceName, "test-init", "test"); err != nil {
			keyringAvailable = false
			return
		}
		_ = gokeyring.Delete(serviceName, "test-init")
		keyringAvailable = true
	})
}

func DisableForTesting() {
	mu.Lock()
	defer mu.Unlock()
	testMode = true
}

func EnableForTesting() {
	mu.Lock()
	defer mu.Unlock()
	testMode = false
}

func IsAvailable() bool {
	ensureInitialized()
	mu.Lock()
	defer mu.Unlock()
	return keyringAvailable && !testMode
}

func SetPeerSecret(peerID, secret string) error {
	if !IsAvailable() {
		return fmt.Errorf("keyring not available")
	}
	return gokeyring.Set(serviceName, "peer-secret-"+peerID, secret)
}

func GetPeerSecret(peerID string) (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("keyring not available")
	}
	return gokeyring.Get(serviceName, "peer-secret-"+peerID)
}

func DeletePeerSecret(peerID string) error {
	if !IsAvailable() {
		return fmt.Errorf("keyring not available")
	}
	return gokeyring.Delete(serviceName, "peer-secret-"+peerID)
}

func SetTLSKey(deviceID, keyPEM string) error {
	if !IsAvailable() {
		return fmt.Errorf("keyring not available")
	}
	return gokeyring.Set(serviceName, "tls-key-"+deviceID, keyPEM)
}

func GetTLSKey(deviceID string) (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("keyring not available")
	}
	return gokeyring.Get(serviceName, "tls-key-"+deviceID)
}

func SetSharedSecret(secret string) error {
	if !IsAvailable() {
		return fmt.Errorf("keyring not available")
	}
	return gokeyring.Set(serviceName, "shared-secret", secret)
}

func GetSharedSecret() (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("keyring not available")
	}
	return gokeyring.Get(serviceName, "shared-secret")
}
