#!/usr/bin/env bash
set -euo pipefail

# Resolve symlinks portably (macOS BSD readlink lacks -f)
SOURCE="${BASH_SOURCE[0]}"
while [ -L "$SOURCE" ]; do
  DIR=$(cd "$(dirname "$SOURCE")" && pwd)
  SOURCE=$(readlink "$SOURCE")
  # If readlink returned a relative path, resolve it
  [[ "$SOURCE" != /* ]] && SOURCE="$DIR/$SOURCE"
done
SCRIPT_DIR=$(cd "$(dirname "$SOURCE")" && pwd)
LLAMA_SERVER="$SCRIPT_DIR/bin/llama-server"
MODEL_DIR="$SCRIPT_DIR/models"
SERVER_LOG="$SCRIPT_DIR/.llama-server.log"

# Set library path for llama-server shared libs
export LD_LIBRARY_PATH="${SCRIPT_DIR}/bin:${LD_LIBRARY_PATH:-}"
export DYLD_LIBRARY_PATH="${SCRIPT_DIR}/bin:${DYLD_LIBRARY_PATH:-}"

# Find model
MODEL="${TERMFIX_MODEL:-}"
if [ -n "$MODEL" ] && [ ! -f "$MODEL" ]; then
  echo "ERROR: TERMFIX_MODEL file not found: $MODEL"
  exit 1
fi
if [ -z "$MODEL" ]; then
  MODEL=$(ls "$MODEL_DIR"/*qwen15b*q4_k_m* 2>/dev/null | head -1 || true)
fi
if [ -z "$MODEL" ]; then
  MODEL=$(ls "$MODEL_DIR"/*.gguf 2>/dev/null | head -1 || true)
fi
if [ -z "$MODEL" ]; then
  echo "ERROR: No model found in $MODEL_DIR"
  echo ""
  echo "Download a model from the GitHub release and place it in:"
  echo "  $MODEL_DIR/"
  exit 1
fi

# Validate port
PORT="${TERMFIX_PORT:-8012}"
if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
  echo "ERROR: Invalid port: $PORT (must be 1-65535)"
  exit 1
fi
export LOCAL_ENDPOINT="http://127.0.0.1:${PORT}"

cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "Starting llama-server on port $PORT with $(basename "$MODEL")..."
"$LLAMA_SERVER" \
  --model "$MODEL" \
  -c 8192 --flash-attn on \
  --cache-type-k q8_0 --cache-type-v q8_0 \
  --host 127.0.0.1 --port "$PORT" \
  --parallel 1 \
  >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

# Wait for server to be ready (up to 60s)
echo -n "Waiting for model to load"
for i in $(seq 1 60); do
  if curl -sf "$LOCAL_ENDPOINT/health" 2>/dev/null | grep -q '"ok"'; then
    echo " ready!"
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo ""
    echo "ERROR: llama-server failed to start. Log output:"
    tail -20 "$SERVER_LOG" 2>/dev/null
    exit 1
  fi
  echo -n "."
  sleep 1
done

# Verify server actually came up (loop may have exhausted without break)
if ! curl -sf "$LOCAL_ENDPOINT/health" 2>/dev/null | grep -q '"ok"'; then
  echo ""
  echo "ERROR: llama-server failed to become ready within 60 seconds. Log output:"
  tail -20 "$SERVER_LOG" 2>/dev/null
  exit 1
fi

# Auto-generate config after server is confirmed ready
MODEL_NAME=$(basename "$MODEL")
REGEN_CONFIG=false
if [ ! -f "$SCRIPT_DIR/.termfix.json" ]; then
  REGEN_CONFIG=true
elif ! grep -q "$MODEL_NAME" "$SCRIPT_DIR/.termfix.json" 2>/dev/null; then
  REGEN_CONFIG=true
fi
if [ "$REGEN_CONFIG" = true ]; then
  cat > "$SCRIPT_DIR/.termfix.json" << EOF
{
  "providers": {
    "local": { "apiKey": "dummy" }
  },
  "agents": {
    "coder":      { "model": "local.${MODEL_NAME}" },
    "summarizer": { "model": "local.${MODEL_NAME}" },
    "task":       { "model": "local.${MODEL_NAME}" },
    "title":      { "model": "local.${MODEL_NAME}", "maxTokens": 80 }
  }
}
EOF
fi

cd "$SCRIPT_DIR"
"$SCRIPT_DIR/bin/termfix" "$@"
