package tee

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPCSCertificateManagement(t *testing.T) {
	// Create a temporary directory for test certificates
	tempDir, err := os.MkdirTemp("", "pcs_certificate_test")
	require.NoError(t, err, "Should create temp directory")
	defer os.RemoveAll(tempDir)

	// Save original env vars and restore after test
	origCertPath := os.Getenv("TDX_PCS_CERT_PATH")
	origCertSaveDir := os.Getenv("TDX_PCS_CERT_SAVE_DIR")
	origTestMode := os.Getenv("TDX_PCS_TEST_MODE")
	defer func() {
		os.Setenv("TDX_PCS_CERT_PATH", origCertPath)
		os.Setenv("TDX_PCS_CERT_SAVE_DIR", origCertSaveDir)
		os.Setenv("TDX_PCS_TEST_MODE", origTestMode)
	}()

	// Set test environment
	os.Setenv("TDX_PCS_CERT_PATH", tempDir)
	os.Setenv("TDX_PCS_CERT_SAVE_DIR", tempDir)
	os.Setenv("TDX_PCS_TEST_MODE", "true")

	// Generate test certificates
	rootCert, rootKey := generateTestCertificate(t, "Intel Root CA", nil, nil, true)
	intermediateCert, intermediateKey := generateTestCertificate(t, "Intel PCK Certificate", rootCert, rootKey, false)
	tcbCert, _ := generateTestCertificate(t, "Intel TCB Signing Certificate", intermediateCert, intermediateKey, false)

	// Save certificates to test directory
	saveCertificateToTestDir(t, rootCert, filepath.Join(tempDir, defaultRootCAFile))
	saveCertificateToTestDir(t, intermediateCert, filepath.Join(tempDir, defaultPCKCertFile))
	saveCertificateToTestDir(t, tcbCert, filepath.Join(tempDir, defaultTCBSigningCertFile))

	// Reset singleton state
	trustConfig = nil
	tcMutex = sync.RWMutex{}

	t.Run("GetPCSTrustConfig", func(t *testing.T) {
		// Get trust configuration
		config, err := GetPCSTrustConfig()
		require.NoError(t, err, "Should get trust config without error")
		require.NotNil(t, config, "Trust config should not be nil")
		
		// Verify certificates were loaded
		assert.NotNil(t, config.RootCert, "Root cert should be loaded")
		assert.NotNil(t, config.PCKCert, "PCK cert should be loaded")
		assert.NotNil(t, config.TCBSigningCert, "TCB signing cert should be loaded")
		
		// Verify certificates match what we generated
		assert.Equal(t, rootCert.SerialNumber.String(), config.RootCert.SerialNumber.String(), "Root cert should match")
		assert.Equal(t, intermediateCert.SerialNumber.String(), config.PCKCert.SerialNumber.String(), "PCK cert should match")
		assert.Equal(t, tcbCert.SerialNumber.String(), config.TCBSigningCert.SerialNumber.String(), "TCB cert should match")
	})

	t.Run("CreateVerificationCertPool", func(t *testing.T) {
		// Create certificate pool
		pool, err := CreateVerificationCertPool()
		require.NoError(t, err, "Should create cert pool without error")
		require.NotNil(t, pool, "Cert pool should not be nil")
		
		// Verify certificates using the pool
		opts := x509.VerifyOptions{
			Roots:     pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}
		
		// Verify intermediate cert
		_, err = intermediateCert.Verify(opts)
		assert.NoError(t, err, "Should verify intermediate cert")
		
		// Verify TCB signing cert
		_, err = tcbCert.Verify(opts)
		assert.NoError(t, err, "Should verify TCB signing cert")
	})

	t.Run("VerifyPCSResponseSignature", func(t *testing.T) {
		// Create test data to sign
		testData := []byte("Test PCS response data")
		
		// Sign the data using the TCB signing key
		hashed := sha256.Sum256(testData)
		signature, err := rsa.SignPKCS1v15(rand.Reader, intermediateKey, crypto.SHA256, hashed[:])
		require.NoError(t, err, "Should sign test data")
		
		// Create a temporary override of the TCB signing certificate with our test cert
		// Save original trust config and restore after test
		origTrustConfig := trustConfig
		defer func() { 
			// Clear mutex and restore original config
			tcMutex = sync.RWMutex{}
			trustConfig = origTrustConfig 
		}()
		
		// Create a new trust config with our test certificates
		tcMutex.Lock()
		trustConfig = &TrustConfig{
			RootCert:       rootCert,
			PCKCert:        intermediateCert,
			TCBSigningCert: intermediateCert, // Use intermediate cert for signing in this test
			CertPool:       x509.NewCertPool(),
			CertCacheDir:   tempDir,
			LastRefresh:    time.Now(),
		}
		tcMutex.Unlock()
		
		// Now verify the signature - CORRECT parameter order
		err = VerifyPCSResponseSignature(testData, signature)
		assert.NoError(t, err, "Should verify valid signature")
		
		// Test with invalid signature
		invalidSignature := make([]byte, len(signature))
		copy(invalidSignature, signature)
		invalidSignature[0] ^= 0xFF // Flip some bits
		
		err = VerifyPCSResponseSignature(invalidSignature, testData)
		assert.Error(t, err, "Should reject invalid signature")
		assert.Contains(t, err.Error(), "verification failed", "Error should explain the issue")
	})

	t.Run("VerifyPCSCertificateChain", func(t *testing.T) {
		// Create a leaf certificate signed by the intermediate
		leafCert, _ := generateTestCertificate(t, "Leaf Certificate", intermediateCert, intermediateKey, false)
		
		// Make sure our test config is using our test certificates
		// Save original trust config and restore after test
		origTrustConfig := trustConfig
		defer func() { 
			// Reset singleton state
			tcMutex = sync.RWMutex{}
			trustConfig = origTrustConfig 
		}()
		
		// Create a new trust config with our test certificates
		tcMutex.Lock()
		trustConfig = &TrustConfig{
			RootCert:       rootCert,
			PCKCert:        intermediateCert,
			TCBSigningCert: tcbCert,
			CertPool:       x509.NewCertPool(),
			CertCacheDir:   tempDir,
			LastRefresh:    time.Now(),
		}
		// Add test certificates to pool
		trustConfig.CertPool.AddCert(rootCert)
		trustConfig.CertPool.AddCert(intermediateCert)
		trustConfig.CertPool.AddCert(tcbCert)
		tcMutex.Unlock()
		
		// Verify the certificate chain
		err := VerifyPCSCertificateChain(leafCert)
		assert.NoError(t, err, "Should verify valid certificate chain")
		
		// Create an invalid certificate (self-signed)
		invalidCert, _ := generateTestCertificate(t, "Invalid Certificate", nil, nil, false)
		
		// Verify the invalid certificate
		err = VerifyPCSCertificateChain(invalidCert)
		assert.Error(t, err, "Should reject invalid certificate")
		assert.Contains(t, err.Error(), "verification failed", "Error should explain the issue")
	})
}

// Helper function to generate test certificates
func generateTestCertificate(t *testing.T, commonName string, parent *x509.Certificate, parentKey *rsa.PrivateKey, isCA bool) (*x509.Certificate, *rsa.PrivateKey) {
	// Generate a private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "Should generate private key")

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err, "Should generate serial number")

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}

	// If this is a self-signed certificate
	if parent == nil {
		parent = &template
		parentKey = privateKey
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, parent, &privateKey.PublicKey, parentKey)
	require.NoError(t, err, "Should create certificate")

	// Parse the certificate
	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err, "Should parse certificate")

	return cert, privateKey
}

// Helper function to save certificate to a file
func saveCertificateToTestDir(t *testing.T, cert *x509.Certificate, certPath string) {
	// Create PEM block
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}

	// Create directory if needed
	dir := filepath.Dir(certPath)
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err, "Should create directory")

	// Write the certificate
	file, err := os.Create(certPath)
	require.NoError(t, err, "Should create file")
	defer file.Close()

	err = pem.Encode(file, block)
	require.NoError(t, err, "Should write certificate to file")
}
