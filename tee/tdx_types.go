// tdx_types.go - Common types and constants for TDX attestation
package tee

// Constants for TDX attestation
const (
	// Quote header and data sizes
	tdxQuoteHeaderSize = 48
	tdxMeasurementOffset = 96
	tdxMeasurementSize = 48  // SHA-384 hash size
	tdxUserDataOffset = 144
	tdxUserDataSize = 64
	tdxReportSize = 512  // Size of the TD report section
	
	// Maximum data sizes for parameter validation
	maxAttestationDataSize = 16 * 1024  // 16KB max
	
	// Intel PCS API endpoints
	intelPCSBaseURL = "https://api.trustedservices.intel.com/tdx/"
	intelPCSQuotePath = "/v1/quotes/verify"
)

// TDXQuote contains the parsed components of a TDX quote
type TDXQuote struct {
	Header      []byte
	ReportData  struct {
		Measurement []byte // MRTD (SHA-384)
		UserData    []byte // Custom data
	}
	Signature   []byte
	Collateral  []byte
}
