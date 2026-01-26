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

# Auto-enable streaming optimization for cloud deployments (Dokploy, etc.)
# This can be controlled via ENABLE_STREAMING environment variable (default: true for cloud deploys)
ENABLE_STREAMING="${ENABLE_STREAMING:-true}"

if [ "$ENABLE_STREAMING" = "true" ] || [ "$DEPLOY" = "cloud" ]; then
    echo "Enabling streaming optimization for cloud deployment..."
    
    # Check if streaming section is commented out
    if grep -q "^# streaming:" "$WORKING_CONFIG" 2>/dev/null; then
        echo "Uncommenting existing streaming section..."
        # Uncomment the streaming section and its parameters
        awk '
        /^# streaming:/ { print "streaming:"; next }
        /^#   keepalive-seconds:/ { print "  keepalive-seconds: 15   # Auto-enabled for cloud deployment"; next }
        /^#   bootstrap-retries:/ { print "  bootstrap-retries: 1    # Auto-enabled for cloud deployment"; next }
        { print }
        ' "$WORKING_CONFIG" > "$WORKING_CONFIG.tmp" && mv "$WORKING_CONFIG.tmp" "$WORKING_CONFIG"
        
        echo "✓ Streaming keep-alive and bootstrap retries enabled"
    elif ! grep -q "^streaming:" "$WORKING_CONFIG" 2>/dev/null; then
        echo "Adding streaming section to config..."
        # Add streaming section after nonstream-keepalive-interval or at the end of global config
        awk '
        /^nonstream-keepalive-interval:/ { 
            print
            print ""
            print "# Streaming behavior (SSE keep-alives + safe bootstrap retries) - Auto-enabled for cloud deployment"
            print "streaming:"
            print "  keepalive-seconds: 15   # Emit heartbeat every 15 seconds to prevent proxy timeouts"
            print "  bootstrap-retries: 1    # Retry once before first byte sent for better reliability"
            next
        }
        { print }
        ' "$WORKING_CONFIG" > "$WORKING_CONFIG.tmp" && mv "$WORKING_CONFIG.tmp" "$WORKING_CONFIG"
        
        echo "✓ Streaming section added to config"
    else
        echo "✓ Streaming section already active in config"
    fi
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

# Override usage-statistics-enabled from environment variable if set
# This ensures the setting persists even if volume is recreated
if [ -n "$USAGE_STATISTICS_ENABLED" ]; then
    echo "Enforcing USAGE_STATISTICS_ENABLED=$USAGE_STATISTICS_ENABLED from environment..."
    
    if grep -q "^usage-statistics-enabled:" "$WORKING_CONFIG" 2>/dev/null; then
        # Replace existing value
        awk -v val="$USAGE_STATISTICS_ENABLED" '
        /^usage-statistics-enabled:/ { print "usage-statistics-enabled: " val; next }
        { print }
        ' "$WORKING_CONFIG" > "$WORKING_CONFIG.tmp" && mv "$WORKING_CONFIG.tmp" "$WORKING_CONFIG"
        echo "✓ usage-statistics-enabled set to $USAGE_STATISTICS_ENABLED"
    else
        # Add new line after logs-max-total-size-mb
        awk -v val="$USAGE_STATISTICS_ENABLED" '
        /^logs-max-total-size-mb:/ { 
            print
            print ""
            print "# Usage statistics - enforced by USAGE_STATISTICS_ENABLED environment variable"
            print "usage-statistics-enabled: " val
            next
        }
        { print }
        ' "$WORKING_CONFIG" > "$WORKING_CONFIG.tmp" && mv "$WORKING_CONFIG.tmp" "$WORKING_CONFIG"
        echo "✓ usage-statistics-enabled added with value $USAGE_STATISTICS_ENABLED"
    fi
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
