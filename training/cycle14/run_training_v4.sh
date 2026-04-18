#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Termfix Cycle 14v4 — Round 2 real-world gap training
# Merges: v3 data (cycle3+v2+v3) + v4 round2 realworld gaps
# Run inside screen: screen -S termfix-train bash run_training_v4.sh
# =============================================================================

CYCLE_DIR="$(cd "$(dirname "$0")" && pwd)"
CYCLE3_DIR="$HOME/termfix/training/cycle3"
PYTHON="$HOME/miniconda3/envs/gptsovits/bin/python3"
LLAMA_CPP="$HOME/llama.cpp"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="$CYCLE_DIR/output/$TIMESTAMP"
LOG="$CYCLE_DIR/logs/train-v4-$TIMESTAMP.log"

mkdir -p "$CYCLE_DIR/data_v4" "$CYCLE_DIR/logs" "$OUTPUT_DIR"

echo "=============================================="
echo "  Termfix Cycle 14v4 Training Pipeline"
echo "  (Round 2 real-world gaps)"
echo "  Started: $(date)"
echo "  Output: $OUTPUT_DIR"
echo "  Log: $LOG"
echo "=============================================="

export PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

# Step 1: Generate v4 data (round 2 realworld gaps)
echo ""
echo "=== Step 1/6: Generating v4 data ==="
$PYTHON "$CYCLE_DIR/generate_realworld_data_v2.py" \
    2>&1 | tee -a "$LOG"

# Step 2: Merge v3 train data + v4 data
echo ""
echo "=== Step 2/6: Merging all training data ==="
$PYTHON -c "
import json, os, random

cycle_dir = '$CYCLE_DIR'
sources = {}
for name, path in [
    ('v3_train', os.path.join(cycle_dir, 'data_v3/train.jsonl')),
    ('v4_train', os.path.join(cycle_dir, 'data_v4/train_rw2.jsonl')),
]:
    data = []
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                if line.strip():
                    data.append(json.loads(line))
    sources[name] = data
    print(f'  {name}: {len(data)} examples')

all_train = sources['v3_train'] + sources['v4_train']
random.seed(44)
random.shuffle(all_train)

# Merge eval/valid from all sources
eval_data = []
valid_data = []
for ddir in ['data_v2', 'data_v3', 'data_v4']:
    for suffix in ['eval.jsonl', 'eval_realworld.jsonl', 'eval_rw2.jsonl']:
        p = os.path.join(cycle_dir, ddir, suffix)
        if os.path.exists(p):
            with open(p) as f:
                for line in f:
                    if line.strip():
                        eval_data.append(json.loads(line))
    for suffix in ['valid.jsonl', 'valid_realworld.jsonl', 'valid_rw2.jsonl']:
        p = os.path.join(cycle_dir, ddir, suffix)
        if os.path.exists(p):
            with open(p) as f:
                for line in f:
                    if line.strip():
                        valid_data.append(json.loads(line))

out_dir = os.path.join(cycle_dir, 'data_v4')
with open(os.path.join(out_dir, 'train.jsonl'), 'w') as f:
    for item in all_train:
        f.write(json.dumps(item) + '\n')
with open(os.path.join(out_dir, 'eval.jsonl'), 'w') as f:
    for item in eval_data:
        f.write(json.dumps(item) + '\n')
with open(os.path.join(out_dir, 'valid.jsonl'), 'w') as f:
    for item in valid_data:
        f.write(json.dumps(item) + '\n')

print(f'\nMerged dataset:')
print(f'  Train: {len(all_train)} examples')
print(f'  Eval:  {len(eval_data)} examples')
print(f'  Valid: {len(valid_data)} examples')
" 2>&1 | tee -a "$LOG"

echo ""
echo "=== Data files ==="
wc -l "$CYCLE_DIR/data_v4/"*.jsonl | tee -a "$LOG"

# Step 3: Clean up old outputs to free disk space
echo ""
echo "=== Step 3/6: Freeing disk space ==="
# Remove v3 merged model dir (we have the gguf already)
rm -rf "$CYCLE_DIR/output/20260415T003228Z/merged" 2>/dev/null && echo "  Removed v3 merged dir" || true
# Remove old checkpoint dirs
find "$CYCLE_DIR/output" -name "checkpoint-*" -type d -exec rm -rf {} + 2>/dev/null || true
echo "  Disk: $(df -h / | tail -1 | awk '{print $4}') available"

# Step 4: Train
echo ""
echo "=== Step 4/6: Training ==="
$PYTHON "$CYCLE3_DIR/train.py" \
    --model "Qwen/Qwen3.5-0.8B" \
    --data-dir "$CYCLE_DIR/data_v4" \
    --output-dir "$OUTPUT_DIR" \
    --epochs 3 \
    --lr 2e-4 \
    --batch-size 1 \
    --grad-accum 16 \
    --max-seq-length 2048 \
    --lora-rank 32 \
    2>&1 | tee -a "$LOG"

# Step 5: Export GGUF
echo ""
echo "=== Step 5/6: Exporting GGUF ==="
GGUF_F16="$OUTPUT_DIR/model-f16.gguf"
GGUF_Q4="$OUTPUT_DIR/termfix-cycle14v4-$TIMESTAMP-q4_k_m.gguf"

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

# Step 6: Run synthetic eval
echo ""
echo "=== Step 6/6: Running evaluation ==="
$PYTHON "$CYCLE_DIR/patch_evaluate.py" \
    "$CYCLE3_DIR/evaluate.py" \
    "$CYCLE_DIR/evaluate_v4.py" \
    2>&1 | tee -a "$LOG"

$PYTHON "$CYCLE_DIR/evaluate_v4.py" \
    --model "$OUTPUT_DIR/merged" \
    --eval-data "$CYCLE_DIR/data_v4/eval.jsonl" \
    --output "$CYCLE_DIR/eval-results-v4" \
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
