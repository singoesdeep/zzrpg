#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -e

# Navigate to the project root directory where docker-compose.yml is located
cd "$(dirname "$0")/.."

echo "=== Initializing zzrpg Infrastructure via Podman ==="

# Check if podman is installed
if ! command -v podman &> /dev/null; then
    echo "Error: podman is not installed. Please install it using: sudo dnf install podman"
    exit 1
fi

# Check if podman-compose is installed
if command -v podman-compose &> /dev/null; then
    echo "Found podman-compose. Starting services..."
    podman-compose up -d
else
    echo "podman-compose not found. Falling back to native podman pod creation..."
    
    # Create pod if it doesn't exist
    if ! podman pod exists zzrpg-pod; then
        echo "Creating podman pod 'zzrpg-pod'..."
        podman pod create --name zzrpg-pod -p 5432:5432 -p 6379:6379
    else
        echo "Podman pod 'zzrpg-pod' already exists."
    fi

    # Start Postgres inside the pod
    if ! podman container exists zzrpg-postgres; then
        echo "Starting PostgreSQL container..."
        podman run -d --pod zzrpg-pod \
            --name zzrpg-postgres \
            -e POSTGRES_USER=postgres \
            -e POSTGRES_PASSWORD=password123 \
            -e POSTGRES_DB=zzrpg \
            -v zzrpg-postgres-data:/var/lib/postgresql/data:Z \
            docker.io/library/postgres:16-alpine
    else
        echo "PostgreSQL container already exists. Starting it..."
        podman start zzrpg-postgres
    fi

    # Start Redis inside the pod
    if ! podman container exists zzrpg-redis; then
        echo "Starting Redis container..."
        podman run -d --pod zzrpg-pod \
            --name zzrpg-redis \
            -v zzrpg-redis-data:/data:Z \
            docker.io/library/redis:7-alpine
    else
        echo "Redis container already exists. Starting it..."
        podman start zzrpg-redis
    fi
fi

echo "=== Infrastructure started successfully ==="
echo "PostgreSQL is running on localhost:5432"
echo "Redis is running on localhost:6379"
