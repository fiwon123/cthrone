#!/bin/bash

# Get the folder where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check if already in PATH
if ! grep -Fxq "export PATH=\"$SCRIPT_DIR:\$PATH\"" "$HOME/.bashrc"; then
    echo "export PATH=\"$SCRIPT_DIR:\$PATH\"" >> "$HOME/.bashrc"
    echo "Folder $SCRIPT_DIR added to PATH in ~/.bashrc"
else
    echo "Folder $SCRIPT_DIR already in PATH"
fi

echo "Run 'source ~/.bashrc' or restart terminal to apply changes."
