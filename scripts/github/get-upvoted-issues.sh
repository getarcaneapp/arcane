#!/bin/bash

# Get token from GitHub CLI if available
if command -v gh &> /dev/null; then
  TOKEN=$(gh auth token 2>/dev/null)
  if [ -z "$TOKEN" ]; then
    echo "Warning: gh CLI found but not authenticated. Run 'gh auth login'"
    TOKEN=""
  else
    echo "Using token from gh CLI"
  fi
else
  echo "Warning: gh CLI not found. Install from https://cli.github.com/"
  TOKEN=""
fi

if ! command -v jq &> /dev/null; then
  echo "Error: jq is required. Install it and try again."
  exit 1
fi

echo "Fetching open issues with most upvotes (label: needs more upvotes)..."
echo ""

# Create temporary file for sorting
temp=$(mktemp)

# Get all OPEN issues with the label and process them (paginate)
page=1
while :; do
  response=$(curl -s ${TOKEN:+-H "Authorization: token $TOKEN"} \
    -H "Accept: application/vnd.github.squirrel-girl-preview+json" \
    "https://api.github.com/repos/getarcaneapp/arcane/issues?labels=needs+more+upvotes&state=open&per_page=100&page=$page")

  if ! echo "$response" | jq -e 'type == "array"' > /dev/null 2>&1; then
    message=$(echo "$response" | jq -r '.message // "Unknown error"')
    echo "Error: GitHub API response was not an array: $message" >&2
    break
  fi

  count=$(echo "$response" | jq 'length')
  if [ "$count" -eq 0 ]; then
    break
  fi

  echo "$response" \
    | jq -r '.[] | "\(.reactions["+1"] // 0)|\(.number)|\(.state)|\(.title)"' \
    >> "$temp"

  page=$((page + 1))
done

if [ ! -s "$temp" ]; then
  echo "No matching issues found (label: needs more upvotes)."
  rm "$temp"
  exit 0
fi

# Sort and display
sort -t'|' -k1 -rn "$temp" | while IFS='|' read -r likes num state title; do
  printf "%3d 👍 - #%-4s [%s] %s\n" "$likes" "$num" "$state" "$title"
  echo "         https://github.com/getarcaneapp/arcane/issues/$num"
  echo ""
done

rm "$temp"
