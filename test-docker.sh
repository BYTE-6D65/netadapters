#!/bin/bash

set -e

echo "🐳 Building Docker images..."
docker-compose build

echo ""
echo "🚀 Starting containers..."
docker-compose up -d

echo ""
echo "⏳ Waiting for services to start..."
sleep 3

echo ""
echo "📊 Container status:"
docker-compose ps

echo ""
echo "📝 Server logs (first 10 lines):"
docker-compose logs echo-server | head -20

echo ""
echo "📝 Client logs (first 10 lines):"
docker-compose logs http-client | head -20

echo ""
echo "✅ Integration test running!"
echo ""
echo "Commands:"
echo "  View live logs:    docker-compose logs -f"
echo "  View server logs:  docker-compose logs -f echo-server"
echo "  View client logs:  docker-compose logs -f http-client"
echo "  Stop containers:   docker-compose down"
echo ""
echo "Press Ctrl+C to stop watching logs, or run 'docker-compose down' to stop containers"
echo ""

# Follow logs
docker-compose logs -f
