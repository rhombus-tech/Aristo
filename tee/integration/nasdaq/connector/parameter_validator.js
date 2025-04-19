/**
 * Parameter Format Validation Module for Dual TEE Cross-Attestation Framework
 * -------------------------------------------------------------------------
 * Handles parameter validation for WebAssembly contracts running in TEE environments.
 * Supports both length-prefixed and direct parameter formats with protection against
 * format detection confusion attacks.
 */

/**
 * Validates and processes parameters for WebAssembly contracts
 * @param {Buffer} buffer - Raw input buffer
 * @param {number} [expectedSize] - Expected size for direct format (optional)
 * @returns {object} - Parameter validation result with format and data
 */
function validateParameter(buffer, expectedSize) {
  if (!Buffer.isBuffer(buffer)) {
    throw new Error("Input must be a Buffer");
  }

  // Apply size limits to prevent DoS attacks
  const MAX_SIZE = 1024; // 1KB limit
  if (buffer.length > MAX_SIZE) {
    throw new Error(`Parameter size (${buffer.length}) exceeds maximum allowed (${MAX_SIZE})`);
  }
  
  // Handle empty buffer case
  if (buffer.length === 0) {
    return { format: "empty", data: buffer };
  }
  
  // For very small buffers, use direct format
  if (buffer.length < 4) {
    console.log("Parameter too short, using direct format");
    return { format: "direct", data: buffer };
  }
  
  // Check if first 4 bytes could be a reasonable length prefix
  const lengthValue = buffer.readUInt32LE(0);
  
  // Validate length-prefixed format:
  // 1. Length must be reasonable (>0 and <= MAX_SIZE)
  // 2. Total buffer size must be adequate (length + 4 bytes prefix)
  if (lengthValue > 0 && lengthValue <= MAX_SIZE && lengthValue + 4 <= buffer.length) {
    console.log(`Detected length-prefixed format, length: ${lengthValue}`);
    return { 
      format: "length-prefixed", 
      data: buffer.slice(4, 4 + lengthValue) 
    };
  } else {
    // If not a valid length prefix, treat as direct format
    console.log("Using direct format, no valid length prefix detected");
    
    // If expectedSize is provided, validate direct format size
    if (expectedSize && buffer.length !== expectedSize) {
      console.warn(`Warning: Direct format size mismatch (expected ${expectedSize}, got ${buffer.length})`);
    }
    
    return { format: "direct", data: buffer };
  }
}

/**
 * Encodes data with a length prefix
 * @param {Buffer|string} data - Data to encode
 * @returns {Buffer} - Length-prefixed data
 */
function encodeLengthPrefixed(data) {
  // Convert string to Buffer if needed
  if (typeof data === 'string') {
    data = Buffer.from(data);
  }
  
  if (!Buffer.isBuffer(data)) {
    throw new Error("Data must be a Buffer or string");
  }
  
  const MAX_SIZE = 1024;
  if (data.length > MAX_SIZE) {
    throw new Error(`Data size (${data.length}) exceeds maximum allowed (${MAX_SIZE})`);
  }
  
  // Create a new buffer with room for the length prefix
  const result = Buffer.alloc(4 + data.length);
  
  // Write the length as a 32-bit little-endian integer
  result.writeUInt32LE(data.length, 0);
  
  // Copy the data after the length prefix
  data.copy(result, 4);
  
  return result;
}

/**
 * Validates cross-attestation between TEE platforms
 * @param {object} primaryAttestation - Primary TEE attestation data
 * @param {object} secondaryAttestation - Secondary TEE attestation data
 * @returns {boolean} - True if attestation is valid
 */
function validateCrossAttestation(primaryAttestation, secondaryAttestation) {
  const startTime = Date.now();
  
  // Validate primary attestation
  if (!primaryAttestation || !primaryAttestation.tee_id || !primaryAttestation.quote) {
    console.error("Invalid primary attestation data");
    return false;
  }
  
  // Validate secondary attestation
  if (!secondaryAttestation || !secondaryAttestation.tee_id || !secondaryAttestation.quote) {
    console.error("Invalid secondary attestation data");
    return false;
  }
  
  // In production, perform actual attestation verification
  // This is a simplified implementation for testing
  const isPrimaryValid = validateAttestationQuote(primaryAttestation.quote);
  const isSecondaryValid = validateAttestationQuote(secondaryAttestation.quote);
  
  const endTime = Date.now();
  const verificationTime = endTime - startTime;
  
  console.log(`Cross-attestation verification completed in ${verificationTime}ms`);
  
  // Check performance target
  if (verificationTime > 100) {
    console.warn(`Verification time (${verificationTime}ms) exceeds 100ms target`);
  }
  
  return isPrimaryValid && isSecondaryValid;
}

/**
 * Helper function to validate attestation quote
 * @param {string} quote - Attestation quote
 * @returns {boolean} - True if quote is valid
 */
function validateAttestationQuote(quote) {
  // In production, implement actual quote validation logic
  // This is a placeholder for testing
  return quote && quote.length > 0;
}

module.exports = {
  validateParameter,
  encodeLengthPrefixed,
  validateCrossAttestation
};
