// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"
	
	"github.com/rhombus-tech/vm/tee/stateless/archive"
)

func main() {
	log.Println("Starting ZK Archival System Example")
	log.Println("===================================")
	log.Println("This example demonstrates how ZK-based state archival")
	log.Println("integrates with the existing stateless blockchain implementation")
	log.Println("while preserving the robust dual-format parameter handling for WebAssembly contracts.")
	log.Println()
	log.Println("Key features:")
	log.Println("1. Maintains robust WebAssembly parameter handling that prevents the 3.5B byte length issue")
	log.Println("2. Dramatically reduces state storage requirements via ZK proofs")
	log.Println("3. Uses recursive proof composition for constant-sized state regardless of chain age")
	log.Println("4. Separates proof generation latency from the critical execution path")
	log.Println("5. Maintains NASDAQ-level compliance with TEE attestation verification")
	log.Println()
	
	// Run the ZK archival example
	archive.RunZKArchivalExample()
}
