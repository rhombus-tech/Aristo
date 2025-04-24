// coordination/channel.go
package coordination

import (
	"crypto/rand"
	"encoding/json"
	"log"
	"sync"
	"time"
)

type SecureChannel struct {
    Worker1   WorkerID    `json:"worker1"`
    Worker2   WorkerID    `json:"worker2"`
    Key       []byte      `json:"key"`
    Encrypted bool        `json:"encrypted"`
    messages  chan []byte // Unexported since it can't be marshaled
    done      chan struct{}
    mu        sync.RWMutex
}

func NewSecureChannel(w1, w2 WorkerID) *SecureChannel {
    return &SecureChannel{
        Worker1:   w1,
        Worker2:   w2,
        messages:  make(chan []byte, 1000), // Increase buffer size to prevent deadlocks
        done:      make(chan struct{}),
        // Initialize with empty key and encryption off for testing
        Key:       make([]byte, 32),
        Encrypted: false,
    }
}

func (c *SecureChannel) EstablishSecure() error {
    // Generate session key
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return err
    }
    
    c.mu.Lock()
    c.Key = key        // Now using exported Key
    c.Encrypted = true // Now using exported Encrypted
    c.mu.Unlock()
    
    return nil
}

// EstablishSecureWithTimeout attempts to establish a secure channel with a timeout
func (c *SecureChannel) EstablishSecureWithTimeout(timeout time.Duration) error {
    log.Printf("Channel: Starting secure channel establishment with timeout %v", timeout)
    
    // Use a channel to signal completion
    done := make(chan error, 1)
    
    // Run the EstablishSecure in a goroutine
    go func() {
        log.Printf("Channel: Goroutine started for EstablishSecure")
        err := c.EstablishSecure()
        log.Printf("Channel: EstablishSecure completed with error: %v", err)
        done <- err
    }()
    
    log.Printf("Channel: Waiting for secure channel establishment or timeout")
    
    // Wait for either completion or timeout
    select {
    case err := <-done:
        if err != nil {
            log.Printf("Channel: Secure channel establishment failed: %v", err)
        } else {
            log.Printf("Channel: Secure channel established successfully")
        }
        return err
    case <-time.After(timeout):
        log.Printf("Channel: Secure channel establishment timed out after %v", timeout)
        return ErrTimeout
    }
}

func (c *SecureChannel) Send(data []byte) error {
    c.mu.RLock()
    encrypted := c.Encrypted // Now using exported Encrypted
    c.mu.RUnlock()

    if encrypted {
        var err error
        data, err = encrypt(data, c.Key) // Now using exported Key
        if err != nil {
            return err
        }
    }

    select {
    case c.messages <- data:
        return nil
    case <-time.After(5 * time.Second):
        return ErrTimeout
    }
}

func (c *SecureChannel) Receive() ([]byte, error) {
    select {
    case data := <-c.messages:
        c.mu.RLock()
        encrypted := c.Encrypted // Now using exported Encrypted
        c.mu.RUnlock()

        if encrypted {
            var err error
            data, err = decrypt(data, c.Key) // Now using exported Key
            if err != nil {
                return nil, err
            }
        }
        return data, nil
    case <-time.After(5 * time.Second):
        return nil, ErrTimeout
    }
}

func (c *SecureChannel) Close() error {
    close(c.done)
    return nil
}

// Add custom marshaling methods
func (c *SecureChannel) MarshalJSON() ([]byte, error) {
    type Alias struct {
        Worker1   WorkerID `json:"worker1"`
        Worker2   WorkerID `json:"worker2"`
        Key       []byte   `json:"key"`
        Encrypted bool     `json:"encrypted"`
    }
    
    return json.Marshal(&Alias{
        Worker1:   c.Worker1,
        Worker2:   c.Worker2,
        Key:       c.Key,
        Encrypted: c.Encrypted,
    })
}

func (c *SecureChannel) UnmarshalJSON(data []byte) error {
    type Alias struct {
        Worker1   WorkerID `json:"worker1"`
        Worker2   WorkerID `json:"worker2"`
        Key       []byte   `json:"key"`
        Encrypted bool     `json:"encrypted"`
    }
    
    aux := &Alias{}
    if err := json.Unmarshal(data, aux); err != nil {
        return err
    }
    
    c.Worker1 = aux.Worker1
    c.Worker2 = aux.Worker2
    c.Key = aux.Key
    c.Encrypted = aux.Encrypted
    
    // Reinitialize channels
    c.messages = make(chan []byte, 100)
    c.done = make(chan struct{})
    
    return nil
}

func encrypt(data, key []byte) ([]byte, error) {
    // For now, we're simply using a XOR-based encryption for testing
    // In production, this would use a proper crypto algorithm like AES-GCM
    result := make([]byte, len(data))
    for i := 0; i < len(data); i++ {
        result[i] = data[i] ^ key[i%len(key)]
    }
    return result, nil
}

func decrypt(data, key []byte) ([]byte, error) {
    // XOR-based decryption (same operation as encryption)
    return encrypt(data, key) // XOR is its own inverse operation
}