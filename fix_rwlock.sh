#!/bin/bash

FILE="/Users/talzisckind/Downloads/aristo-fresh 2/execution/controller/src/discovery_service.rs"
BACKUP="${FILE}.bak_script"

# Create a backup
cp "$FILE" "$BACKUP"

# Fix read().await
sed -i '' -e '/\.read()\.await;/ {
s/\(let [^=]*\) = \([^.]*\)\.read()\.await;/\1 = match \2\.read() {\
            Ok(guard) => guard,\
            Err(e) => {\
                log::error!("Failed to acquire read lock: {}", e);\
                return Err(TeeError::IoError(std::io::Error::new(std::io::ErrorKind::Other, e.to_string())));\
            }\
        };/g
}' "$FILE"

# Fix write().await
sed -i '' -e '/\.write()\.await;/ {
s/\(let [^=]*\) = \([^.]*\)\.write()\.await;/\1 = match \2\.write() {\
            Ok(guard) => guard,\
            Err(e) => {\
                log::error!("Failed to acquire write lock: {}", e);\
                return Err(TeeError::IoError(std::io::Error::new(std::io::ErrorKind::Other, e.to_string())));\
            }\
        };/g
}' "$FILE"

# Fix if let Ok(mut XXX) = YYY.write().await
sed -i '' -e 's/if let Ok(mut \([^)]*\)) = \([^.]*\)\.write()\.await {/if let Ok(mut \1) = \2\.write() {/g' "$FILE"

# Fix if let Ok(XXX) = YYY.read().await
sed -i '' -e 's/if let Ok(\([^)]*\)) = \([^.]*\)\.read()\.await {/if let Ok(\1) = \2\.read() {/g' "$FILE"

echo "RwLock fixes applied successfully."
