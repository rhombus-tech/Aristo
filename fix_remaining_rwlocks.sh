#!/bin/bash

FILE="/Users/talzisckind/Downloads/aristo-fresh 2/execution/controller/src/discovery_service.rs"

# Fix pattern: if let Ok(mut XXX) = YYY.write().await {
sed -i '' 's/if let Ok(mut \([^)]*\)) = \([^.]*\)\.write()\.await {/if let Ok(mut \1) = \2\.write() {/g' "$FILE"

echo "Fixed remaining RwLock issues"
