#!/bin/sh
set -e

ORIGINAL_CONFIG="/CLIProxyAPI/config.yaml"
EXAMPLE_CONFIG="/CLIProxyAPI/config.example.yaml"
WORKING_CONFIG=""

# Determine which config to use as base
if [ -d "$ORIGINAL_CONFIG" ]; then
    # config.yaml is a directory (Docker mount issue) - use temp location
    echo "Warning: config.yaml is a directory (volume mount issue), using temporary config"
    WORKING_CONFIG="/tmp/config.yaml"
    cp "$EXAMPLE_CONFIG" "$WORKING_CONFIG"
elif [ -f "$ORIGINAL_CONFIG" ]; then
    # config.yaml exists and is a regular file - use it in place
    echo "Using existing config.yaml"
    WORKING_CONFIG="$ORIGINAL_CONFIG"
else
    # config.yaml doesn't exist - create from example
    echo "Config file not found, creating from config.example.yaml"
    cp "$EXAMPLE_CONFIG" "$ORIGINAL_CONFIG"
    WORKING_CONFIG="$ORIGINAL_CONFIG"
fi

# If MANAGEMENT_PASSWORD is set, inject it into the config
if [ -n "$MANAGEMENT_PASSWORD" ]; then
    echo "Injecting MANAGEMENT_PASSWORD from environment variable..."

    # Use sed to update the secret-key line, preserving indentation
    # This matches the line with "secret-key:" and replaces the entire line
    sed -i 's/^  secret-key:.*/  secret-key: "'"$MANAGEMENT_PASSWORD"'"/' "$WORKING_CONFIG"

    # Also enable remote access for Dokploy deployments
    sed -i 's/^  allow-remote:.*/  allow-remote: true/' "$WORKING_CONFIG"

    # Ensure control panel is NOT disabled
    sed -i 's/^  disable-control-panel:.*/  disable-control-panel: false/' "$WORKING_CONFIG"

    echo "Management password configured, remote access enabled, and control panel enabled"
fi

# Execute the main application with config path if needed
if [ "$WORKING_CONFIG" != "$ORIGINAL_CONFIG" ]; then
    # Config is in non-default location, pass it as argument
    exec "$@" -config "$WORKING_CONFIG"
else
    # Config is in default location, run normally
    exec "$@"
fi
