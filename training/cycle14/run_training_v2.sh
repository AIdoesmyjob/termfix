#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Termfix Cycle 14v2 — Retrain without ls tool
# Run inside screen: screen -S termfix-train bash run_training_v2.sh
# =============================================================================

CYCLE_DIR="$(cd "$(dirname "$0")" && pwd)"
CYCLE3_DIR="$HOME/termfix/training/cycle3"
PYTHON="$HOME/miniconda3/envs/gptsovits/bin/python3"
LLAMA_CPP="$HOME/llama.cpp"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="$CYCLE_DIR/output/$TIMESTAMP"
LOG="$CYCLE_DIR/logs/train-v2-$TIMESTAMP.log"

mkdir -p "$CYCLE_DIR/data_v2" "$CYCLE_DIR/logs" "$OUTPUT_DIR"

echo "=============================================="
echo "  Termfix Cycle 14v2 Training Pipeline"
echo "  (No ls tool, 4 tools only)"
echo "  Started: $(date)"
echo "  Output: $OUTPUT_DIR"
echo "  Log: $LOG"
echo "=============================================="

export PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

# Step 1: Remove ls from cycle3 training data
echo ""
echo "=== Step 1/6: Removing ls tool from cycle3 data ==="
$PYTHON "$CYCLE_DIR/remove_ls_tool.py" \
    "$CYCLE3_DIR/data/train.jsonl" \
    "$CYCLE_DIR/data_v2/cycle3_no_ls.jsonl" \
    2>&1 | tee -a "$LOG"

# Step 2: Generate v2 training data (already has no ls)
echo ""
echo "=== Step 2/6: Generating v2 training data ==="
$PYTHON "$CYCLE_DIR/generate_v2_data.py" \
    --output-dir "$CYCLE_DIR/data_v2" \
    --variations 4 \
    --merge-with "$CYCLE_DIR/data_v2/cycle3_no_ls.jsonl" \
    2>&1 | tee -a "$LOG"

echo ""
echo "=== Data files ==="
wc -l "$CYCLE_DIR/data_v2/"*.jsonl | tee -a "$LOG"

# Step 3: Train
echo ""
echo "=== Step 3/6: Training ==="
$PYTHON "$CYCLE3_DIR/train.py" \
    --model "Qwen/Qwen3.5-0.8B" \
    --data-dir "$CYCLE_DIR/data_v2" \
    --output-dir "$OUTPUT_DIR" \
    --epochs 3 \
    --lr 2e-4 \
    --batch-size 1 \
    --grad-accum 16 \
    --max-seq-length 2048 \
    --lora-rank 32 \
    2>&1 | tee -a "$LOG"

# Step 4: Export GGUF
echo ""
echo "=== Step 4/6: Exporting GGUF ==="
GGUF_F16="$OUTPUT_DIR/model-f16.gguf"
GGUF_Q4="$OUTPUT_DIR/termfix-cycle14v2-$TIMESTAMP-q4_k_m.gguf"

echo "  Converting to f16 GGUF..."
$PYTHON "$LLAMA_CPP/convert_hf_to_gguf.py" \
    "$OUTPUT_DIR/merged" \
    --outfile "$GGUF_F16" \
    --outtype f16 \
    2>&1 | tee -a "$LOG"

echo "  Quantizing to Q4_K_M..."
"$LLAMA_CPP/build/bin/llama-quantize" \
    "$GGUF_F16" "$GGUF_Q4" Q4_K_M \
    2>&1 | tee -a "$LOG"

echo "  Removing f16 intermediate..."
rm -f "$GGUF_F16"

# Step 5: Patch eval script and run eval
echo ""
echo "=== Step 5/6: Patching eval script ==="
$PYTHON "$CYCLE_DIR/patch_evaluate.py" \
    "$CYCLE3_DIR/evaluate.py" \
    "$CYCLE_DIR/evaluate_v2.py" \
    2>&1 | tee -a "$LOG"

echo ""
echo "=== Step 6/6: Running evaluation ==="
$PYTHON "$CYCLE_DIR/evaluate_v2.py" \
    --model "$OUTPUT_DIR/merged" \
    --eval-data "$CYCLE_DIR/data_v2/eval.jsonl" \
    --output "$CYCLE_DIR/eval-results-v2" \
    2>&1 | tee -a "$LOG"

# Summary
echo ""
echo "=============================================="
echo "  Training Complete!"
echo "  GGUF: $GGUF_Q4"
echo "  Size: $(du -h "$GGUF_Q4" 2>/dev/null | cut -f1)"
echo "  Log:  $LOG"
echo "  Finished: $(date)"
echo "=============================================="
echo ""
echo "To copy model to your Mac:"
echo "  scp vm:$GGUF_Q4 ~/termfix/models/"
