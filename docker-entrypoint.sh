#!/bin/sh
set -e

# Handle the case where Docker creates a directory when config.yaml doesn't exist on host
if [ -d /CLIProxyAPI/config.yaml ]; then
    echo "Config path is a directory (Docker auto-created), removing and creating file..."
    rm -rf /CLIProxyAPI/config.yaml
    cp /CLIProxyAPI/config.example.yaml /CLIProxyAPI/config.yaml
elif [ ! -f /CLIProxyAPI/config.yaml ]; then
    echo "Config file not found, copying from config.example.yaml..."
    cp /CLIProxyAPI/config.example.yaml /CLIProxyAPI/config.yaml
fi

# Execute the main application
exec "$@"
