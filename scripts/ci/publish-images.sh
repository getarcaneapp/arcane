#!/usr/bin/env bash
set -euo pipefail

case "$IMAGE_REGISTRY" in
    ghcr.io | docker.io | quay.io | public.ecr.aws) ;;
    *)
        echo 'Unsupported registry' >&2
        exit 1
        ;;
esac
for digest in "$MANAGER_DIGEST" "$AGENT_DIGEST"; do
    if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
        echo "Invalid canonical digest: $digest" >&2
        exit 1
    fi
done

case "$IMAGE_CHANNEL" in
    next)
        if [[ ! "$IMAGE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+-next\.[0-9]+$ ]]; then
            echo "Invalid next image version: '$IMAGE_VERSION' (expected X.Y.Z-next.N)" >&2
            exit 1
        fi
        moving_tag=next
        tags=("v$IMAGE_VERSION" next)
        if [[ "$IMAGE_REGISTRY" == public.ecr.aws ]]; then
            tags=(next)
        fi
        ;;
    release)
        if [[ ! "$IMAGE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            echo "Invalid release image version: '$IMAGE_VERSION' (expected X.Y.Z)" >&2
            exit 1
        fi
        IFS=. read -r major minor _ <<<"$IMAGE_VERSION"
        moving_tag=latest
        tags=("v$IMAGE_VERSION" "v$major.$minor" "v$major" latest)
        ;;
    *)
        echo 'Unsupported release channel' >&2
        exit 1
        ;;
esac

# An old retry must not move the destination tags backward.
for role in manager agent; do
    if [[ "$role" == manager ]]; then
        expected=$MANAGER_DIGEST
    else
        expected=$AGENT_DIGEST
    fi
    actual=$(oras resolve "ghcr.io/getarcaneapp/$role:$moving_tag")
    if [[ "$actual" != "$expected" ]]; then
        echo "Obsolete publishing run for $role: expected=$expected actual=$actual" >&2
        exit 1
    fi
done

copy_log=$(mktemp)
trap 'rm "$copy_log"' EXIT
for role in manager agent; do
    if [[ "$role" == manager ]]; then
        digest=$MANAGER_DIGEST
    else
        digest=$AGENT_DIGEST
    fi
    source_image="ghcr.io/getarcaneapp/$role@$digest"
    destination="$IMAGE_REGISTRY/getarcaneapp/$role"

    copied=false
    # GHCR briefly reports existing child manifests as not found, so only auth failures are final.
    for delay in 0 10 30 60 90; do
        if [[ "$delay" != 0 ]]; then
            sleep "$delay"
        fi
        if oras cp --recursive "$source_image" "$destination:${tags[0]}" >"$copy_log" 2>&1; then
            cat "$copy_log"
            copied=true
            break
        fi
        cat "$copy_log" >&2
        if grep -Eqi 'status code 40[13]|unauthorized|denied|forbidden' "$copy_log"; then
            break
        fi
    done
    if [[ "$copied" != true ]]; then
        echo "Copy failed: $source_image to $destination:${tags[0]}" >&2
        exit 1
    fi

    if [[ "${#tags[@]}" -gt 1 ]]; then
        oras tag "$destination@$digest" "${tags[@]:1}"
    fi
    cosign sign --key env://COSIGN_PRIVATE_KEY --yes "$destination@$digest"
    printf '%s tags=%s digest=%s copied and signed\n' "$destination" "${tags[*]}" "$digest" | tee -a "$GITHUB_STEP_SUMMARY"
done
