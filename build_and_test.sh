#!/bin/bash
set -e

echo "Copying WASM files..."
mkdir -p ./hyper/x/contracts/test/fixtures
cp ./hyper/target/wasm32-unknown-unknown/release/call_contract.wasm ./hyper/x/contracts/test/fixtures/call_contract.wasm
cp ./hyper/target/wasm32-unknown-unknown/release/deploy_contract.wasm ./hyper/x/contracts/test/fixtures/deploy_contract.wasm

echo "Running import_contract_test..."
cd ./hyper
go test -v ./x/contracts/runtime -run "TestImportContractDeployContract|TestImportContractCallContractActor"
