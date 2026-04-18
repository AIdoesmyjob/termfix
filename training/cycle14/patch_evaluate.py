#!/usr/bin/env python3
"""Patch the cycle3 evaluate.py to:
1. Remove 'ls' from TOOLS_DEFINITION
2. Accept multiple valid tools per test case (via 'alt_tool_names' field)
"""

import re
import sys

def patch(src: str) -> str:
    # 1. Remove ls from TOOLS_DEFINITION
    # Find the ls tool block and remove it
    src = re.sub(
        r',?\s*\{"type": "function", "function": \{"name": "ls".*?\}\}',
        '',
        src,
        flags=re.DOTALL
    )

    # 2. Remove ls from TOOL_NAMES set
    src = src.replace(
        'TOOL_NAMES = {"bash", "view", "grep", "glob", "ls"}',
        'TOOL_NAMES = {"bash", "view", "grep", "glob"}'
    )

    # 3. Add alt_tool_names support in evaluate_example
    # Replace the tool selection check to also accept alt_tool_names
    src = src.replace(
        '''            result["tool_selection_correct"] = first_call["name"] == expected_tool''',
        '''            alt_tools = expected.get("alt_tool_names", [])
            valid_tools = {expected_tool} | set(alt_tools)
            result["tool_selection_correct"] = first_call["name"] in valid_tools'''
    )

    # 4. Also fix the argument check to work with alt tools
    src = src.replace(
        '''            if first_call["name"] == expected_tool:''',
        '''            if first_call["name"] in valid_tools:'''
    )

    return src

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <input.py> <output.py>")
        sys.exit(1)

    with open(sys.argv[1]) as f:
        src = f.read()

    patched = patch(src)

    with open(sys.argv[2], "w") as f:
        f.write(patched)

    print(f"Patched {sys.argv[1]} -> {sys.argv[2]}")
    print(f"  - Removed ls from TOOLS_DEFINITION")
    print(f"  - Removed ls from TOOL_NAMES")
    print(f"  - Added alt_tool_names support")
