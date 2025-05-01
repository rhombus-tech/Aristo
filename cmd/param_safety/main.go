// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	
	"github.com/rhombus-tech/vm/tee/stateless/archive"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("Testing parameter handling safety for ZK archival...")
	
	// Normal length-prefixed parameter (20 bytes)
	goodParam := make([]byte, 24) // 4 byte length + 20 bytes data
	binary.LittleEndian.PutUint32(goodParam[:4], 20)
	for i := 4; i < 24; i++ {
		goodParam[i] = byte(i)
	}
	
	// Malicious parameter with 3.5 billion length prefix
	badParam := make([]byte, 8) // Just need 4 bytes for length + a few more
	binary.LittleEndian.PutUint32(badParam[:4], 3500000000) // 3.5 billion!
	
	// Direct format parameter (no length prefix)
	directParam := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	
	fmt.Println("\n--- Testing normal length-prefixed parameter ---")
	testParam(goodParam)
	
	fmt.Println("\n--- Testing direct format parameter (no length prefix) ---")
	testParam(directParam)
	
	fmt.Println("\n--- Testing malicious parameter (3.5 billion length) ---")
	testParam(badParam)
}

func testParam(param []byte) {
	// Use our robust parameter handling from the utilities.go file
	parsed, format, err := archive.ParseDualFormatParameter(param, true, true)
	
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	
	fmt.Printf("Parameter successfully parsed using %s format\n", format)
	fmt.Printf("Original length: %d bytes\n", len(param))
	fmt.Printf("Parsed length: %d bytes\n", len(parsed))
	
	// In a real contract, this would be the point where we'd read memory
	// but our robust parameter handling prevents WebAssembly traps
}
