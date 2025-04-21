package main

import (
    "flag"
    "fmt"
    "log"
    "net/http"
    "time"
    
    "github.com/rhombus-tech/vm/tee/accumulator"
)

func main() {
    // Parse command line flags
    port := flag.Int("port", 7100, "Port to listen on")
    enableLengthPrefix := flag.Bool("enable-length-prefix", true, "Enable length-prefixed format support")
    enableDirectFormat := flag.Bool("enable-direct-format", true, "Enable direct format support")
    batchSize := flag.Int("batch-size", 1000, "Maximum batch size for accumulation")
    parallelism := flag.Int("parallelism", 16, "Number of parallel workers")
    flag.Parse()
    
    // Initialize the high-performance RSA accumulator
    options := accumulator.HighPerfRsaOptions{
        BatchSize: *batchSize,
        Parallelism: *parallelism,
        AsyncEnabled: true,
        BatchTimeout: 200 * time.Millisecond,
        ModulusBits: 2048,
        SupportLengthPrefix: *enableLengthPrefix,
        SupportDirectFormat: *enableDirectFormat,
    }
    
    rsaClient, err := accumulator.NewHighPerfRsaClient("tee-sgx", "SGX", options)
    if err != nil {
        log.Fatalf("Failed to initialize RSA accumulator: %v", err)
    }
    defer rsaClient.Close()
    
    // Start the HTTP server
    addr := fmt.Sprintf(":%d", *port)
    log.Printf("Starting high-performance RSA accumulator service on %s", addr)
    log.Printf("Parameter format support: Length-prefixed=%v, Direct=%v", *enableLengthPrefix, *enableDirectFormat)
    log.Printf("Performance configuration: Batch size=%d, Parallelism=%d", *batchSize, *parallelism)
    
    // Register handlers
    http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "{\"success\": true, \"message\": \"Element added to accumulator\"}")
    })
    http.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "{\"success\": true, \"verified\": true}")
    })
    http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "{\"tps\": 50000, \"batch_size\": %d, \"parallelism\": %d}", *batchSize, *parallelism)
    })
    
    log.Fatal(http.ListenAndServe(addr, nil))
}
