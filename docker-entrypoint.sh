#!/bin/sh
set -e

ORIGINAL_CONFIG="/CLIProxyAPI/config/config.yaml"
EXAMPLE_CONFIG="/CLIProxyAPI/config.example.yaml"
WORKING_CONFIG=""

# Ensure the config directory exists (for volume mounts)
mkdir -p /CLIProxyAPI/config

# Determine which config to use as base
if [ -f "$ORIGINAL_CONFIG" ]; then
    # config.yaml exists and is a regular file - use it in place
    echo "Using existing config.yaml from persistent volume"
    WORKING_CONFIG="$ORIGINAL_CONFIG"
else
    # config.yaml doesn't exist - create from example
    echo "Config file not found in persistent volume, creating from config.example.yaml"
    cp "$EXAMPLE_CONFIG" "$ORIGINAL_CONFIG"
    WORKING_CONFIG="$ORIGINAL_CONFIG"
fi

# If MANAGEMENT_PASSWORD is set, inject it into the config
if [ -n "$MANAGEMENT_PASSWORD" ]; then
    echo "Injecting MANAGEMENT_PASSWORD from environment variable..."

    # Use awk to modify the YAML file - more reliable than sed in Alpine
    awk -v pwd="$MANAGEMENT_PASSWORD" '
    /^  secret-key:/ { print "  secret-key: \"" pwd "\""; next }
    /^  allow-remote:/ { print "  allow-remote: true"; next }
    /^  disable-control-panel:/ { print "  disable-control-panel: false"; next }
    { print }
    ' "$WORKING_CONFIG" > "$WORKING_CONFIG.tmp" && mv "$WORKING_CONFIG.tmp" "$WORKING_CONFIG"

    echo "Management password configured, remote access enabled, and control panel enabled"

    # Debug: show the remote-management section
    echo "Config remote-management section:"
    grep -A 8 "^remote-management:" "$WORKING_CONFIG" || echo "Failed to read config"
fi

# Execute the main application
if [ -n "$WORKING_CONFIG" ]; then
    # Always pass the config path to ensure the application finds it in the persistent volume
    echo "Starting application with config: $WORKING_CONFIG"
    exec "$@" -config "$WORKING_CONFIG"
else
    # Fallback to default behavior
    exec "$@"
fi
