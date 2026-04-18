#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Termfix Cycle 14 — Full training pipeline
# Run inside screen: screen -S termfix-train bash run_training.sh
# =============================================================================

CYCLE_DIR="$(cd "$(dirname "$0")" && pwd)"
CYCLE3_DIR="$HOME/termfix/training/cycle3"
PYTHON="$HOME/miniconda3/envs/gptsovits/bin/python3"
LLAMA_CPP="$HOME/llama.cpp"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="$CYCLE_DIR/output/$TIMESTAMP"
LOG="$CYCLE_DIR/logs/train-$TIMESTAMP.log"

mkdir -p "$CYCLE_DIR/data" "$CYCLE_DIR/logs" "$OUTPUT_DIR"

echo "=============================================="
echo "  Termfix Cycle 14 Training Pipeline"
echo "  Started: $(date)"
echo "  Output: $OUTPUT_DIR"
echo "  Log: $LOG"
echo "=============================================="

# Step 1: Generate v2 training data and merge with cycle3
echo ""
echo "=== Step 1/5: Generating training data ==="
$PYTHON "$CYCLE_DIR/generate_v2_data.py" \
    --output-dir "$CYCLE_DIR/data" \
    --variations 4 \
    --merge-with "$CYCLE3_DIR/data/train.jsonl" \
    2>&1 | tee -a "$LOG"

echo ""
echo "=== Data files ==="
wc -l "$CYCLE_DIR/data/"*.jsonl | tee -a "$LOG"

# Step 2: Train with PEFT LoRA (same as cycle3 but with merged data)
echo ""
echo "=== Step 2/5: Training ==="
$PYTHON "$CYCLE3_DIR/train.py" \
    --model "Qwen/Qwen3.5-0.8B" \
    --data-dir "$CYCLE_DIR/data" \
    --output-dir "$OUTPUT_DIR" \
    --epochs 3 \
    --lr 2e-4 \
    --batch-size 1 \
    --grad-accum 16 \
    --max-seq-length 2048 \
    --lora-rank 32 \
    2>&1 | tee -a "$LOG"

# Step 3: Export GGUF
echo ""
echo "=== Step 3/5: Exporting GGUF ==="
GGUF_PATH="$OUTPUT_DIR/termfix-cycle14-$TIMESTAMP-q4_k_m.gguf"
$PYTHON "$CYCLE3_DIR/export_gguf.py" \
    --model "$OUTPUT_DIR/merged" \
    --output "$GGUF_PATH" \
    --quant q4_k_m \
    --method llama-cpp \
    2>&1 | tee -a "$LOG"

# Step 4: Start llama-server for evaluation
echo ""
echo "=== Step 4/5: Starting llama-server for eval ==="
EVAL_PORT=8099
LLAMA_SERVER="$LLAMA_CPP/build/bin/llama-server"
if [ ! -f "$LLAMA_SERVER" ]; then
    LLAMA_SERVER="$LLAMA_CPP/llama-server"
fi

# Kill any existing eval server
pkill -f "llama-server.*$EVAL_PORT" 2>/dev/null || true
sleep 2

$LLAMA_SERVER \
    -m "$GGUF_PATH" \
    --port $EVAL_PORT \
    --temp 0.7 --top-p 0.8 --top-k 20 --repeat-penalty 1.5 \
    --reasoning off --reasoning-format deepseek \
    -c 8192 --flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 \
    --jinja --parallel 1 \
    > "$CYCLE_DIR/logs/llama-server-$TIMESTAMP.log" 2>&1 &

LLAMA_PID=$!
echo "llama-server PID: $LLAMA_PID (port $EVAL_PORT)"

# Wait for server to be ready
echo "Waiting for llama-server to load model..."
for i in $(seq 1 120); do
    if curl -s "http://localhost:$EVAL_PORT/health" | grep -q "ok" 2>/dev/null; then
        echo "  Server ready after ${i}s"
        break
    fi
    if ! kill -0 $LLAMA_PID 2>/dev/null; then
        echo "  ERROR: llama-server died. Check logs/llama-server-$TIMESTAMP.log"
        cat "$CYCLE_DIR/logs/llama-server-$TIMESTAMP.log" | tail -20
        exit 1
    fi
    sleep 1
done

# Step 5: Run eval harness
echo ""
echo "=== Step 5/5: Running evaluation ==="
if [ -f "$HOME/termfix/training/eval_harness.py" ]; then
    $PYTHON "$HOME/termfix/training/eval_harness.py" \
        --server "http://localhost:$EVAL_PORT" \
        --verbose \
        2>&1 | tee -a "$LOG"
elif [ -f "$CYCLE3_DIR/evaluate.py" ]; then
    $PYTHON "$CYCLE3_DIR/evaluate.py" \
        --model "$OUTPUT_DIR/merged" \
        --eval-data "$CYCLE_DIR/data/eval.jsonl" \
        --output "$CYCLE_DIR/eval-results" \
        2>&1 | tee -a "$LOG"
else
    echo "  WARNING: No eval script found, skipping evaluation"
fi

# Cleanup
echo ""
echo "Stopping llama-server..."
kill $LLAMA_PID 2>/dev/null || true

# Copy best model
echo ""
echo "=============================================="
echo "  Training Complete!"
echo "  GGUF: $GGUF_PATH"
echo "  Size: $(du -h "$GGUF_PATH" 2>/dev/null | cut -f1)"
echo "  Log:  $LOG"
echo "  Finished: $(date)"
echo "=============================================="
echo ""
echo "To copy model to your Mac:"
echo "  scp vm:$GGUF_PATH ~/termfix/models/"
