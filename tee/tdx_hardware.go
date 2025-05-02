// tdx_hardware.go - Direct hardware communication for TDX attestation
package tee

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// TDX device file for quote generation
const (
	TDX_GUEST_DEVICE = "/dev/tdx-guest"
	
	// IOCTL commands for TDX quote generation
	TDX_CMD_GET_REPORT = 0xA0
	TDX_CMD_GET_QUOTE  = 0xA1
	
	// Sizes and limits
	TDX_REPORT_SIZE      = 1024
	TDX_REPORTDATA_SIZE  = 64
	TDX_QUOTE_MIN_SIZE   = 512
	TDX_QUOTE_MAX_SIZE   = 8 * 1024 // 8KB max
)

// TdxReportRequest represents a request to generate a TD report
type TdxReportRequest struct {
	Reportdata [TDX_REPORTDATA_SIZE]byte
	ReportBuf  [TDX_REPORT_SIZE]byte
}

// TdxQuoteRequest represents a request to generate a TDX quote
type TdxQuoteRequest struct {
	Report     [TDX_REPORT_SIZE]byte
	QuoteBuf   []byte
	QuoteSize  uint32
}

// TDXQuote is now defined in tdx_types.go

// ioctl is a helper function for making ioctl calls to the TDX device
func tdx_ioctl(fd uintptr, request uintptr, argp unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(argp))
	if errno != 0 {
		return fmt.Errorf("ioctl error: %d", errno)
	}
	return nil
}

// tdx_get_report gets a TD report from the TDX device
func tdx_get_report(fd uintptr, req *TdxReportRequest) error {
	// Dual-format parameter validation
	if req == nil {
		return fmt.Errorf("invalid report request: nil pointer")
	}

	// Call the ioctl to get the report
	if err := tdx_ioctl(fd, uintptr(TDX_CMD_GET_REPORT), unsafe.Pointer(req)); err != nil {
		return fmt.Errorf("failed to get TD report: %w", err)
	}
	
	return nil
}

// tdx_get_quote gets a TDX quote from the TDX device
func tdx_get_quote(fd uintptr, req *TdxQuoteRequest) error {
	// Dual-format parameter validation
	if req == nil {
		return fmt.Errorf("invalid quote request: nil pointer")
	}
	
	if req.QuoteSize == 0 || req.QuoteSize > TDX_QUOTE_MAX_SIZE {
		return fmt.Errorf("invalid quote buffer size: %d", req.QuoteSize)
	}
	
	// Call the ioctl to get the quote
	if err := tdx_ioctl(fd, uintptr(TDX_CMD_GET_QUOTE), unsafe.Pointer(req)); err != nil {
		return fmt.Errorf("failed to get TDX quote: %w", err)
	}
	
	return nil
}

// GetTdxQuote generates a TDX quote with provided report data and robust parameter validation
func GetTdxQuote(reportData []byte) (*TDXQuote, error) {
	// Parameter validation
	if len(reportData) == 0 {
		return nil, fmt.Errorf("empty report data")
	}
	
	if len(reportData) > TDX_REPORTDATA_SIZE {
		return nil, fmt.Errorf("report data too large: %d > %d", len(reportData), TDX_REPORTDATA_SIZE)
	}
	
	// Open the TDX device
	tdxDevice, err := os.Open(TDX_GUEST_DEVICE)
	if err != nil {
		return nil, fmt.Errorf("failed to open TDX device: %w", err)
	}
	defer tdxDevice.Close()
	
	// Get TD report
	var reportReq TdxReportRequest
	copy(reportReq.Reportdata[:], reportData)
	
	if err := tdx_get_report(tdxDevice.Fd(), &reportReq); err != nil {
		return nil, fmt.Errorf("failed to get TD report: %w", err)
	}
	
	// Allocate quote buffer
	quoteBuffer := make([]byte, TDX_QUOTE_MAX_SIZE)
	
	// Get TDX quote
	quoteReq := TdxQuoteRequest{
		QuoteBuf:  quoteBuffer,
		QuoteSize: TDX_QUOTE_MAX_SIZE,
	}
	copy(quoteReq.Report[:], reportReq.ReportBuf[:])
	
	if err := tdx_get_quote(tdxDevice.Fd(), &quoteReq); err != nil {
		return nil, fmt.Errorf("failed to get TDX quote: %w", err)
	}
	
	// Parse the quote
	quote, err := ParseTdxQuote(quoteBuffer[:quoteReq.QuoteSize])
	if err != nil {
		return nil, fmt.Errorf("failed to parse TDX quote: %w", err)
	}
	
	return quote, nil
}

// ParseTdxQuote parses a TDX quote buffer with robust parameter validation
func ParseTdxQuote(quoteData []byte) (*TDXQuote, error) {
	// Parameter validation
	if len(quoteData) < TDX_QUOTE_MIN_SIZE {
		return nil, fmt.Errorf("quote data too small: %d < %d", len(quoteData), TDX_QUOTE_MIN_SIZE)
	}
	
	if len(quoteData) > TDX_QUOTE_MAX_SIZE {
		return nil, fmt.Errorf("quote data too large: %d > %d", len(quoteData), TDX_QUOTE_MAX_SIZE)
	}
	
	// Check for length-prefixed format
	var actualQuoteData []byte
	if len(quoteData) >= 4 {
		prefixLen := binary.LittleEndian.Uint32(quoteData[:4])
		if prefixLen > 0 && prefixLen <= TDX_QUOTE_MAX_SIZE && int(prefixLen) <= len(quoteData)-4 {
			// It's in length-prefixed format
			actualQuoteData = quoteData[4:4+prefixLen]
		} else {
			// Not in length-prefixed format, use direct
			actualQuoteData = quoteData
		}
	} else {
		// Not enough data for prefix, use direct
		actualQuoteData = quoteData
	}
	
	// Parse quote components
	// TDX quote has a specific format:
	// - Header (version, type, etc.)
	// - TD Report (includes measurement)
	// - Signature
	// - Collateral (QE identity, TCB info, etc.)
	
	// Using constants from tdx_types.go
	
	// Ensure we have enough data
	if len(actualQuoteData) < tdxQuoteHeaderSize + tdxReportSize {
		return nil, fmt.Errorf("incomplete quote data")
	}
	
	quote := &TDXQuote{
		Header: actualQuoteData[:tdxQuoteHeaderSize],
	}
	
	// Extract measurement (MRTD) from TD report
	if len(actualQuoteData) >= tdxMeasurementOffset + tdxMeasurementSize {
		quote.ReportData.Measurement = make([]byte, tdxMeasurementSize)
		copy(quote.ReportData.Measurement, actualQuoteData[tdxMeasurementOffset:tdxMeasurementOffset+tdxMeasurementSize])
	} else {
		return nil, fmt.Errorf("incomplete quote data: missing measurement")
	}
	
	// Extract user data from TD report
	if len(actualQuoteData) >= tdxUserDataOffset + tdxUserDataSize {
		quote.ReportData.UserData = make([]byte, tdxUserDataSize)
		copy(quote.ReportData.UserData, actualQuoteData[tdxUserDataOffset:tdxUserDataOffset+tdxUserDataSize])
	}
	
	// Extract signature and collateral
	// In a real implementation, we would parse these according to the TDX quote format
	// For now, we'll just capture the remaining data
	if len(actualQuoteData) > tdxQuoteHeaderSize + tdxReportSize {
		signatureOffset := tdxQuoteHeaderSize + tdxReportSize
		signatureSize := 64 // Placeholder, actual size depends on algorithm
		
		if len(actualQuoteData) >= signatureOffset + signatureSize {
			quote.Signature = make([]byte, signatureSize)
			copy(quote.Signature, actualQuoteData[signatureOffset:signatureOffset+signatureSize])
			
			// Collateral is all remaining data
			if len(actualQuoteData) > signatureOffset + signatureSize {
				collateralOffset := signatureOffset + signatureSize
				quote.Collateral = make([]byte, len(actualQuoteData) - collateralOffset)
				copy(quote.Collateral, actualQuoteData[collateralOffset:])
			}
		}
	}
	
	return quote, nil
}

// ValidateTdxQuote performs security validation on a TDX quote
func ValidateTdxQuote(quote *TDXQuote, expectedMeasurement []byte) error {
	// Parameter validation
	if quote == nil {
		return fmt.Errorf("nil quote")
	}
	
	if len(quote.ReportData.Measurement) != tdxMeasurementSize {
		return fmt.Errorf("invalid measurement size: %d != %d", 
			len(quote.ReportData.Measurement), tdxMeasurementSize)
	}
	
	// Verify measurement matches expected value (if provided)
	if len(expectedMeasurement) > 0 {
		if !bytes.Equal(quote.ReportData.Measurement, expectedMeasurement) {
			return fmt.Errorf("measurement mismatch")
		}
	}
	
	// In a real implementation, we would verify the quote signature
	// using the Intel QE (Quoting Enclave) public key
	
	return nil
}
