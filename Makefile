# Makefile for TEE Parameter Validation

.PHONY: all clean build-tee-benchmark build-validator build-proxy build-wasm deploy-validator deploy-sgx-proxy deploy-sev-proxy test

# Configuration
SGX_ENDPOINTS = ec2-54-225-41-220.compute-1.amazonaws.com ec2-52-54-181-245.compute-1.amazonaws.com
SEV_ENDPOINTS = ec2-3-81-65-148.compute-1.amazonaws.com ec2-54-161-2-145.compute-1.amazonaws.com
KEY_FILE = $(HOME)/nasdaq-tee-key.pem
SSH_USER = ubuntu

all: build-tee-benchmark build-validator build-proxy build-wasm

# Build TEE benchmark tool
build-tee-benchmark:
	@echo "Building TEE benchmark tool..."
	GOOS=linux GOARCH=amd64 go build -o bin/tee_benchmark ./cmd/tee_benchmark/

# Build the parameter validator
build-validator:
	@echo "Building parameter validator service..."
	GOOS=linux GOARCH=amd64 go build -o bin/parameter_validator ./cmd/parameter_validator/

# Build the accumulator proxy
build-proxy:
	@echo "Building accumulator proxy service..."
	@echo "Building SGX variant..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/accumulator_proxy_sgx ./cmd/accumulator_proxy/
	@echo "Building SEV variant..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/accumulator_proxy_sev ./cmd/accumulator_proxy/
	@echo "Building local test variant..."
	go build -o bin/accumulator_proxy ./cmd/accumulator_proxy/

# Build the WebAssembly RSA accumulator
build-wasm:
	@echo "Building WebAssembly RSA accumulator..."
	@if [ -d "execution/accumulator/target/wasm32-wasi/release" ]; then \
		cd execution/accumulator && RUSTFLAGS="-C target-feature=+crt-static" cargo build --release --target wasm32-wasi; \
		cp execution/accumulator/target/wasm32-wasi/release/rsa_accumulator.wasm bin/; \
	else \
		echo "WebAssembly build directory not found, using existing module"; \
		cp execution/controller/resources/no_op/target/wasm32-wasip1/release/no_op.wasm bin/rsa_accumulator.wasm; \
	fi

# Deploy validator to SGX nodes
deploy-sgx-validator: build-validator
	@echo "Deploying validator to SGX nodes..."
	@for endpoint in $(SGX_ENDPOINTS); do \
		echo "Deploying to $$endpoint..."; \
		scp -i $(KEY_FILE) bin/parameter_validator $(SSH_USER)@$$endpoint:~/; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl stop python-validator || true"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mv ~/parameter_validator /usr/local/bin/"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl start go-validator || true"; \
	done

# Deploy validator to SEV nodes
deploy-sev-validator: build-validator
	@echo "Deploying validator to SEV nodes..."
	@for endpoint in $(SEV_ENDPOINTS); do \
		echo "Deploying to $$endpoint..."; \
		scp -i $(KEY_FILE) bin/parameter_validator $(SSH_USER)@$$endpoint:~/; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl stop python-validator || true"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mv ~/parameter_validator /usr/local/bin/"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl start go-validator || true"; \
	done

# Deploy proxy to SGX nodes
deploy-sgx-proxy: build-proxy build-wasm
	@echo "Deploying accumulator proxy to SGX nodes..."
	@if [ ! -f deploy/sgx/config.env ]; then \
		mkdir -p deploy/sgx; \
		echo "PORT=7101" > deploy/sgx/config.env; \
		echo "TEE_TYPE=sgx" >> deploy/sgx/config.env; \
		echo "TEE_ID=prod-sgx-accumulator" >> deploy/sgx/config.env; \
		echo "ENABLE_CROSS_VALIDATE=true" >> deploy/sgx/config.env; \
		echo "PEER_ENDPOINTS=$(word 1,$(SEV_ENDPOINTS)):7101" >> deploy/sgx/config.env; \
		echo "MAX_PARAMETER_SIZE=1024" >> deploy/sgx/config.env; \
		echo "CONTRACT_ID_SIZE=32" >> deploy/sgx/config.env; \
		echo "BATCH_SIZE=250" >> deploy/sgx/config.env; \
		echo "MAX_PARALLEL_BATCHES=8" >> deploy/sgx/config.env; \
		echo "MAX_CACHE_SIZE=1024" >> deploy/sgx/config.env; \
		echo "ENABLE_LENGTH_PREFIX=true" >> deploy/sgx/config.env; \
		echo "ENABLE_DIRECT_FORMAT=true" >> deploy/sgx/config.env; \
		echo "PREFETCH=true" >> deploy/sgx/config.env; \
		cp execution/accumulator/enarx_config.toml deploy/sgx/ || echo "Notice: enarx_config.toml not found"; \
	fi
	@for endpoint in $(SGX_ENDPOINTS); do \
		echo "Deploying to $$endpoint..."; \
		scp -i $(KEY_FILE) bin/accumulator_proxy_sgx $(SSH_USER)@$$endpoint:~/accumulator_proxy; \
		scp -i $(KEY_FILE) bin/rsa_accumulator.wasm $(SSH_USER)@$$endpoint:~/; \
		scp -i $(KEY_FILE) deploy/sgx/config.env $(SSH_USER)@$$endpoint:~/; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mkdir -p /opt/wasmlanche/accumulator; sudo cp ~/accumulator_proxy /opt/wasmlanche/accumulator/; sudo cp ~/rsa_accumulator.wasm /opt/wasmlanche/accumulator/; sudo cp ~/config.env /opt/wasmlanche/accumulator/; sudo systemctl restart accumulator-proxy || echo 'Service not yet installed'"; \
	done

# Deploy proxy to SEV nodes
deploy-sev-proxy: build-proxy build-wasm
	@echo "Deploying accumulator proxy to SEV nodes..."
	@for endpoint in $(SEV_ENDPOINTS); do \
		echo "Deploying to $$endpoint..."; \
		scp -i $(KEY_FILE) bin/accumulator_proxy $(SSH_USER)@$$endpoint:~/; \
		scp -i $(KEY_FILE) bin/rsa_accumulator.wasm $(SSH_USER)@$$endpoint:~/; \
		scp -i $(KEY_FILE) enarx_config.toml $(SSH_USER)@$$endpoint:~/; \
		scp -i $(KEY_FILE) deployment/accumulator-proxy.service $(SSH_USER)@$$endpoint:~/; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl stop enarx-accumulator || true"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mv ~/accumulator_proxy /usr/local/bin/"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mv ~/rsa_accumulator.wasm /home/$(SSH_USER)/"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mv ~/enarx_config.toml /home/$(SSH_USER)/"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo mv ~/accumulator-proxy.service /etc/systemd/system/"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl daemon-reload"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl enable accumulator-proxy"; \
		ssh -i $(KEY_FILE) $(SSH_USER)@$$endpoint "sudo systemctl start accumulator-proxy"; \
	done

# Deploy to all nodes
deploy-all: deploy-sgx-validator deploy-sev-validator
	@echo "Deployment completed to all nodes"

# Run tests
test:
	go test -v ./coordination/tests/

# Clean build artifacts
clean:
	rm -rf bin/
