#!/usr/bin/env bash
# Setup development environment for bgpipe
# This script clones the bgpfix dependency with BMP support

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
SRC_DIR="$PROJECT_ROOT/.src"
BGPFIX_DIR="$SRC_DIR/bgpfix"

echo "Setting up bgpipe development environment..."

# Create .src directory if it doesn't exist
if [ ! -d "$SRC_DIR" ]; then
    echo "Creating .src directory..."
    mkdir -p "$SRC_DIR"
fi

# Clone or update bgpfix
if [ -d "$BGPFIX_DIR" ]; then
    echo "Updating bgpfix repository..."
    cd "$BGPFIX_DIR"
    git fetch origin || { echo "Failed to fetch from origin"; exit 1; }
    git checkout -B dev0123 origin/dev0123 || { echo "Failed to checkout dev0123"; exit 1; }
else
    echo "Cloning bgpfix repository (dev0123 branch with BMP support)..."
    cd "$SRC_DIR"
    git clone --single-branch --branch dev0123 https://github.com/bgpfix/bgpfix.git || { echo "Failed to clone bgpfix"; exit 1; }
fi

echo "Running go mod tidy..."
cd "$PROJECT_ROOT"
go mod tidy

echo "Building bgpipe..."
go build -v

echo ""
echo "✅ Development environment setup complete!"
echo ""
echo "The bgpfix dependency with BMP support is now available in .src/bgpfix"
echo "You can now build and run bgpipe with rv-live support."
