#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo ">> .env not found — copying from .env.example"
  cp .env.example .env
fi

echo ">> git pull"
git pull --ff-only

echo ">> docker compose build"
docker compose build

echo ">> docker compose up -d"
docker compose up -d

echo ">> status"
docker compose ps