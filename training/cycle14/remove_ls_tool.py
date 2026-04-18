#!/usr/bin/env python3
"""Remove 'ls' tool from training data and convert ls calls to glob/bash alternatives."""

import json
import sys
import copy
from pathlib import Path


def convert_ls_to_glob(ls_args: dict) -> dict:
    """Convert an ls tool call to a glob tool call."""
    path = ls_args.get("path", ".")
    return {"pattern": "*", "path": path}


def transform_example(example: dict) -> dict:
    """Transform a single training example to remove ls tool."""
    ex = copy.deepcopy(example)

    # 1. Remove ls from tools list
    if "tools" in ex:
        ex["tools"] = [t for t in ex["tools"]
                       if t.get("function", {}).get("name") != "ls"]

    # 2. Remove ls mentions from system prompt
    for msg in ex.get("messages", []):
        if msg.get("role") == "system" and msg.get("content"):
            content = msg["content"]
            # Remove "ls" from tool lists in system prompts
            content = content.replace(", ls)", ")")
            content = content.replace("(bash, view, grep, glob, ls)", "(bash, view, grep, glob)")
            content = content.replace("bash, view, grep, glob, ls", "bash, view, grep, glob")
            content = content.replace("- Use `ls` to list directory contents and see what files exist\n", "")
            content = content.replace("- Use `ls` to list directory contents\n", "")
            msg["content"] = content

    # 3. Convert ls tool calls to glob
    for msg in ex.get("messages", []):
        if msg.get("tool_calls"):
            for tc in msg["tool_calls"]:
                func = tc.get("function", {})
                if func.get("name") == "ls":
                    func["name"] = "glob"
                    old_args = func.get("arguments", {})
                    func["arguments"] = convert_ls_to_glob(old_args)

    # 4. Update expected tool name
    if ex.get("expected", {}).get("tool_name") == "ls":
        ex["expected"]["tool_name"] = "glob"
        old_args = ex["expected"].get("arguments", {})
        ex["expected"]["arguments"] = convert_ls_to_glob(old_args)

    return ex


def main():
    if len(sys.argv) < 3:
        print(f"Usage: {sys.argv[0]} <input.jsonl> <output.jsonl>")
        sys.exit(1)

    input_path = Path(sys.argv[1])
    output_path = Path(sys.argv[2])

    examples = []
    ls_converted = 0
    total = 0

    for line in input_path.read_text().strip().split("\n"):
        if not line.strip():
            continue
        ex = json.loads(line)
        total += 1

        # Check if this example uses ls
        uses_ls = False
        if ex.get("expected", {}).get("tool_name") == "ls":
            uses_ls = True
        for msg in ex.get("messages", []):
            for tc in msg.get("tool_calls", []):
                if tc.get("function", {}).get("name") == "ls":
                    uses_ls = True

        transformed = transform_example(ex)
        examples.append(transformed)

        if uses_ls:
            ls_converted += 1

    with open(output_path, "w") as f:
        for ex in examples:
            f.write(json.dumps(ex, ensure_ascii=False) + "\n")

    print(f"Total examples: {total}")
    print(f"ls → glob converted: {ls_converted}")
    print(f"Output: {output_path}")

    # Verify no ls references remain
    output_text = output_path.read_text()
    remaining = output_text.count('"name": "ls"')
    remaining += output_text.count('"tool_name": "ls"')
    print(f"Remaining ls references: {remaining}")
    if remaining > 0:
        print("WARNING: Some ls references remain!")


if __name__ == "__main__":
    main()
