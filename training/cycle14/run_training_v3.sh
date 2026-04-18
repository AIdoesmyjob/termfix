#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Termfix Cycle 14v3 — Retrain with real-world gap coverage
# Merges: cycle3 data (no ls) + v2 new recipes + v3 realworld gaps
# Run inside screen: screen -S termfix-train bash run_training_v3.sh
# =============================================================================

CYCLE_DIR="$(cd "$(dirname "$0")" && pwd)"
CYCLE3_DIR="$HOME/termfix/training/cycle3"
PYTHON="$HOME/miniconda3/envs/gptsovits/bin/python3"
LLAMA_CPP="$HOME/llama.cpp"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="$CYCLE_DIR/output/$TIMESTAMP"
LOG="$CYCLE_DIR/logs/train-v3-$TIMESTAMP.log"

mkdir -p "$CYCLE_DIR/data_v3" "$CYCLE_DIR/logs" "$OUTPUT_DIR"

echo "=============================================="
echo "  Termfix Cycle 14v3 Training Pipeline"
echo "  (Real-world gap coverage + merged data)"
echo "  Started: $(date)"
echo "  Output: $OUTPUT_DIR"
echo "  Log: $LOG"
echo "=============================================="

export PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

# Step 1: Remove ls from cycle3 training data (if not already done)
echo ""
echo "=== Step 1/7: Preparing cycle3 data (no ls) ==="
if [ -f "$CYCLE_DIR/data_v2/cycle3_no_ls.jsonl" ]; then
    echo "  Using existing cycle3_no_ls.jsonl"
else
    $PYTHON "$CYCLE_DIR/remove_ls_tool.py" \
        "$CYCLE3_DIR/data/train.jsonl" \
        "$CYCLE_DIR/data_v3/cycle3_no_ls.jsonl" \
        2>&1 | tee -a "$LOG"
fi

# Step 2: Generate v2 data (new recipes — if not already done)
echo ""
echo "=== Step 2/7: Preparing v2 data (new recipes) ==="
if [ -f "$CYCLE_DIR/data_v2/train.jsonl" ]; then
    echo "  Using existing v2 train.jsonl"
else
    $PYTHON "$CYCLE_DIR/generate_v2_data.py" \
        --output-dir "$CYCLE_DIR/data_v2" \
        --variations 4 \
        2>&1 | tee -a "$LOG"
fi

# Step 3: Generate v3 data (realworld gap coverage)
echo ""
echo "=== Step 3/7: Generating v3 realworld data ==="
$PYTHON "$CYCLE_DIR/generate_realworld_data.py" \
    2>&1 | tee -a "$LOG"

# Step 4: Merge all training data
echo ""
echo "=== Step 4/7: Merging all training data ==="
$PYTHON -c "
import json, os, random

cycle_dir = '$CYCLE_DIR'

# Load all data sources
sources = {}
for name, path in [
    ('cycle3', os.path.join(cycle_dir, 'data_v2/cycle3_no_ls.jsonl')),
    ('v2_train', os.path.join(cycle_dir, 'data_v2/train.jsonl')),
    ('v3_train', os.path.join(cycle_dir, 'data_v3/train_realworld.jsonl')),
]:
    data = []
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                if line.strip():
                    data.append(json.loads(line))
    sources[name] = data
    print(f'  {name}: {len(data)} examples')

# Merge train
all_train = sources['cycle3'] + sources['v2_train'] + sources['v3_train']
random.seed(42)
random.shuffle(all_train)

# Load eval/valid from v2 and v3
eval_data = []
valid_data = []
for prefix, ddir in [('v2', 'data_v2'), ('v3', 'data_v3')]:
    for name, target in [('eval', eval_data), ('valid', valid_data)]:
        path_candidates = [
            os.path.join(cycle_dir, ddir, f'{name}.jsonl'),
            os.path.join(cycle_dir, ddir, f'{name}_realworld.jsonl'),
        ]
        for p in path_candidates:
            if os.path.exists(p):
                with open(p) as f:
                    for line in f:
                        if line.strip():
                            target.append(json.loads(line))
                break

# Write merged data
out_dir = os.path.join(cycle_dir, 'data_v3')
os.makedirs(out_dir, exist_ok=True)

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
print(f'  Output: {out_dir}/')
" 2>&1 | tee -a "$LOG"

echo ""
echo "=== Data files ==="
wc -l "$CYCLE_DIR/data_v3/"*.jsonl | tee -a "$LOG"

# Step 5: Train
echo ""
echo "=== Step 5/7: Training ==="
$PYTHON "$CYCLE3_DIR/train.py" \
    --model "Qwen/Qwen3.5-0.8B" \
    --data-dir "$CYCLE_DIR/data_v3" \
    --output-dir "$OUTPUT_DIR" \
    --epochs 3 \
    --lr 2e-4 \
    --batch-size 1 \
    --grad-accum 16 \
    --max-seq-length 2048 \
    --lora-rank 32 \
    2>&1 | tee -a "$LOG"

# Step 6: Export GGUF
echo ""
echo "=== Step 6/7: Exporting GGUF ==="
GGUF_F16="$OUTPUT_DIR/model-f16.gguf"
GGUF_Q4="$OUTPUT_DIR/termfix-cycle14v3-$TIMESTAMP-q4_k_m.gguf"

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

# Step 7: Run eval
echo ""
echo "=== Step 7/7: Running evaluation ==="
# Patch eval script to remove ls
$PYTHON "$CYCLE_DIR/patch_evaluate.py" \
    "$CYCLE3_DIR/evaluate.py" \
    "$CYCLE_DIR/evaluate_v3.py" \
    2>&1 | tee -a "$LOG"

$PYTHON "$CYCLE_DIR/evaluate_v3.py" \
    --model "$OUTPUT_DIR/merged" \
    --eval-data "$CYCLE_DIR/data_v3/eval.jsonl" \
    --output "$CYCLE_DIR/eval-results-v3" \
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
