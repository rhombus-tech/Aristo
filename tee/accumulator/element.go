package accumulator

// AccumulatorElement defines an element for the high-performance accumulator
type AccumulatorElement struct {
	// ID is a unique identifier for this element
	ID string

	// Data is the actual element data (e.g., attestation)
	Data []byte

	// Type indicates the type of element (e.g., "SGX", "SEV")
	Type string

	// Timestamp indicates when this element was created
	Timestamp int64

	// Region indicates which region this element belongs to
	Region string
}
