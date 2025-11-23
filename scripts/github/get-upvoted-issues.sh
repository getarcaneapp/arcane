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

echo "Fetching open Feature issues with most upvotes..."
echo ""

# Create temporary file for sorting
temp=$(mktemp)

# Get all OPEN issues with the label and process them
curl -s ${TOKEN:+-H "Authorization: token $TOKEN"} \
  -H "Accept: application/vnd.github.v3+json" \
  "https://api.github.com/repos/getarcaneapp/arcane/issues?labels=need+more+upvotes&state=open&per_page=100" \
  | jq -r '.[] | select(.title | startswith("⚡️ Feature:")) | "\(.number)|\(.title)|\(.state)"' \
  | while IFS='|' read -r num title state; do
    # Get reactions for this issue
    reactions=$(curl -s ${TOKEN:+-H "Authorization: token $TOKEN"} \
      -H "Accept: application/vnd.github.squirrel-girl-preview+json" \
      "https://api.github.com/repos/getarcaneapp/arcane/issues/$num/reactions")

    # Check if reactions is an array (successful response) or error message
    if echo "$reactions" | jq -e 'type == "array"' > /dev/null 2>&1; then
      likes=$(echo "$reactions" | jq '[.[] | select(.content == "+1")] | length')
    else
      # If error or rate limited, set to -1 to indicate error
      likes="-1"
      echo "Warning: Could not fetch reactions for issue #$num (possible rate limit)" >&2
    fi

    echo "$likes|$num|$state|$title" >> "$temp"

    # Small delay to avoid rate limiting
    sleep 0.5
  done

# Sort and display
sort -t'|' -k1 -rn "$temp" | while IFS='|' read -r likes num state title; do
  if [ "$likes" = "-1" ]; then
    printf "  ? 👍 - #%-4s [%s] %s\n" "$num" "$state" "$title"
  else
    printf "%3d 👍 - #%-4s [%s] %s\n" "$likes" "$num" "$state" "$title"
  fi
  echo "         https://github.com/getarcaneapp/arcane/issues/$num"
  echo ""
done

rm "$temp"
