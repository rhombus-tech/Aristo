use std::io::{self, Read, Write};
use std::net::{TcpListener, TcpStream};
use num_bigint::{BigUint, RandBigInt};
use num_integer::Integer;
use num_traits::{One, Zero};
use sha2::{Sha256, Digest};
use rand::{Rng, thread_rng};

// Helper function to generate large primes
fn generate_prime(bit_size: usize) -> BigUint {
    let mut rng = thread_rng();
    
    // Generate a random odd number of the specified bit length
    let mut n: BigUint;
    loop {
        // Generate a random number of bit_size bits
        n = rng.gen_biguint(bit_size as u64);
        
        // Ensure it's odd (set lowest bit to 1)
        if !n.is_odd() {
            n |= BigUint::one();
        }
        
        // Ensure it has the correct bit size
        if n.bits() as usize == bit_size {
            break;
        }
    }
    
    // Simple primality test - Miller-Rabin with 10 rounds
    // This is a simplified version - a real implementation would use more thorough testing
    while !is_probably_prime(&n, 10) {
        // If not prime, add 2 to try the next odd number
        n += BigUint::from(2u32);
    }
    
    n
}

// Basic Miller-Rabin primality test
fn is_probably_prime(n: &BigUint, rounds: usize) -> bool {
    // Handle small cases
    if n <= &BigUint::from(1u32) {
        return false;
    }
    if n <= &BigUint::from(3u32) {
        return true;
    }
    if n.is_even() {
        return false;
    }
    
    // Write n-1 as 2^r * d where d is odd
    let mut d = n - BigUint::one();
    let mut r = 0;
    
    while (&d).is_even() {
        d >>= 1;
        r += 1;
    }
    
    // Witness loop
    let mut rng = thread_rng();
    'witness: for _ in 0..rounds {
        // Choose random witness a in [2, n-2]
        let a = rng.gen_biguint_range(&BigUint::from(2u32), &(n - BigUint::from(2u32)));
        
        // Compute a^d mod n
        let mut x = a.modpow(&d, n);
        
        if x == BigUint::one() || x == (n - BigUint::one()) {
            continue 'witness;
        }
        
        for _ in 0..r-1 {
            x = x.modpow(&BigUint::from(2u32), n);
            if x == (n - BigUint::one()) {
                continue 'witness;
            }
        }
        
        return false;
    }
    
    true
}

// Dual-format parameter validation configuration
const ENABLE_LENGTH_PREFIX_FORMAT: bool = true;
const ENABLE_DIRECT_FORMAT: bool = true;
const MAX_PARAM_SIZE: usize = 1048576; // 1MB max parameter size
const DEFAULT_PORT: u16 = 7300;

// RSA Accumulator parameters
const RSA_KEY_SIZE: usize = 2048;
const BATCH_SIZE: usize = 1000;

struct RsaAccumulator {
    n: BigUint,
    g: BigUint,
}

impl RsaAccumulator {
    fn new() -> Self {
        println!("Generating RSA parameters (this might take a minute)...");
        
        // Generate RSA modulus n = p * q
        let p = generate_prime(RSA_KEY_SIZE / 2);
        println!("Generated first prime p");
        
        let q = generate_prime(RSA_KEY_SIZE / 2);
        println!("Generated second prime q");
        
        let n = &p * &q;
        
        // Generate base g
        let g = BigUint::from(2u32);
        
        RsaAccumulator { n, g }
    }
    
    fn add_to_accumulator(&self, data: &[u8]) -> BigUint {
        let x = BigUint::from_bytes_be(data);
        self.g.modpow(&x, &self.n)
    }
    
    fn batch_add(&self, data_batch: &[Vec<u8>]) -> BigUint {
        let mut acc = self.g.clone();
        
        for data in data_batch {
            let x = BigUint::from_bytes_be(data);
            acc = acc.modpow(&x, &self.n);
        }
        
        acc
    }
    
    fn verify(&self, data: &[u8], witness: &BigUint, acc: &BigUint) -> bool {
        let x = BigUint::from_bytes_be(data);
        let computed = witness.modpow(&x, &self.n);
        &computed == acc
    }
}

// Dual-format parameter parsing
fn parse_dual_format(data: &[u8]) -> Result<&[u8], String> {
    // Check if we have a length prefix format
    if ENABLE_LENGTH_PREFIX_FORMAT && data.len() >= 4 {
        let length = u32::from_le_bytes([data[0], data[1], data[2], data[3]]) as usize;
        
        // Validate length
        if length <= data.len() - 4 && length <= MAX_PARAM_SIZE {
            println!("Using length-prefixed format: {length} bytes");
            return Ok(&data[4..4+length]);
        }
    }
    
    // Fall back to direct format if enabled
    if ENABLE_DIRECT_FORMAT {
        if data.len() <= MAX_PARAM_SIZE {
            println!("Using direct data format: {} bytes", data.len());
            return Ok(data);
        } else {
            return Err(format!("Direct data exceeds maximum allowed size: {} > {}", 
                              data.len(), MAX_PARAM_SIZE));
        }
    }
    
    Err("Invalid parameter format and direct format not enabled".to_string())
}

fn handle_connection(accumulator: &RsaAccumulator, mut stream: TcpStream) {
    let mut buffer = [0; 1024];
    let mut data_batch: Vec<Vec<u8>> = Vec::new();
    
    match stream.read(&mut buffer) {
        Ok(size) => {
            if size > 0 {
                match parse_dual_format(&buffer[0..size]) {
                    Ok(data) => {
                        // Check command
                        if data.len() > 0 {
                            match data[0] {
                                // Add to accumulator
                                1 => {
                                    if data.len() > 1 {
                                        let result = accumulator.add_to_accumulator(&data[1..]);
                                        let response = result.to_bytes_be();
                                        stream.write_all(&response).unwrap();
                                    }
                                },
                                // Batch add
                                2 => {
                                    // Parse batch of items
                                    let mut pos = 1;
                                    while pos < data.len() {
                                        let item_len = data[pos] as usize;
                                        pos += 1;
                                        
                                        if pos + item_len <= data.len() {
                                            data_batch.push(data[pos..pos+item_len].to_vec());
                                            pos += item_len;
                                        }
                                    }
                                    
                                    let result = accumulator.batch_add(&data_batch);
                                    let response = result.to_bytes_be();
                                    stream.write_all(&response).unwrap();
                                },
                                // Verify
                                3 => {
                                    if data.len() > 65 { // Enough bytes for command + data + witness
                                        let element_size = data[1] as usize;
                                        let witness_start = 2 + element_size;
                                        
                                        if witness_start + 256 <= data.len() { // 2048-bit witness = 256 bytes
                                            let element = &data[2..witness_start];
                                            let witness_bytes = &data[witness_start..witness_start+256];
                                            let witness = BigUint::from_bytes_be(witness_bytes);
                                            
                                            let acc_bytes = &data[witness_start+256..];
                                            let acc = BigUint::from_bytes_be(acc_bytes);
                                            
                                            let result = accumulator.verify(element, &witness, &acc);
                                            let response = if result { b"1" } else { b"0" };
                                            stream.write_all(response).unwrap();
                                        }
                                    }
                                },
                                // Health check
                                4 => {
                                    stream.write_all(b"OK").unwrap();
                                },
                                _ => {
                                    stream.write_all(b"Invalid command").unwrap();
                                }
                            }
                        }
                    },
                    Err(err) => {
                        let error_msg = format!("Parameter validation error: {}", err);
                        stream.write_all(error_msg.as_bytes()).unwrap();
                    }
                }
            }
        },
        Err(e) => {
            println!("Error reading from connection: {}", e);
        }
    }
}

fn main() -> io::Result<()> {
    println!("Starting RSA Accumulator with dual-format parameter validation");
    println!("Length-prefix format: {}", if ENABLE_LENGTH_PREFIX_FORMAT { "enabled" } else { "disabled" });
    println!("Direct format: {}", if ENABLE_DIRECT_FORMAT { "enabled" } else { "disabled" });
    
    // Get port from environment variable or use default
    let port = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse::<u16>().ok())
        .unwrap_or(DEFAULT_PORT);
    
    println!("Creating RSA accumulator...");
    let accumulator = RsaAccumulator::new();
    println!("RSA accumulator created successfully");
    
    let addr = format!("0.0.0.0:{}", port);
    println!("Listening on {}", addr);
    let listener = TcpListener::bind(addr)?;
    
    for stream in listener.incoming() {
        match stream {
            Ok(stream) => {
                println!("New connection established");
                handle_connection(&accumulator, stream);
            }
            Err(e) => {
                println!("Error accepting connection: {}", e);
            }
        }
    }
    
    Ok(())
}

// Required entry point for WASI
#[no_mangle]
pub extern "C" fn _start() {
    main().unwrap();
}
