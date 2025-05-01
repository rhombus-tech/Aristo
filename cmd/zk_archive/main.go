// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"

	"github.com/rhombus-tech/vm/tee/stateless/archive"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("Starting ZK archival integration test...")
	
	// Run the ZK archival example which demonstrates integration with dual-format parameter handling
	archive.RunZKArchivalExample()
	
	log.Println("Test completed successfully")
}
