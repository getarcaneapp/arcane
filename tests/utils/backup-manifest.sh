# Prints one tab-separated line per path under /v:
#   path, type, mode, owner, size, mtime, content
# content is the sha256 of a file or the target of a symlink.
# "-" marks a field that does not apply to that type.
cd /v

find . | LC_ALL=C sort | while IFS= read -r path; do
	owner=$(stat -c %u:%g "$path")
	mtime=$(stat -c %Y "$path")
	mode=-
	size=-
	content=-

	if [ -L "$path" ]; then
		type=link
		content=$(readlink "$path")
	elif [ -d "$path" ]; then
		type=dir
		mode=$(stat -c %a "$path")
	else
		type=file
		mode=$(stat -c %a "$path")
		size=$(stat -c %s "$path")
		content=$(sha256sum "$path" | cut -d' ' -f1)
	fi

	printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$path" "$type" "$mode" "$owner" "$size" "$mtime" "$content"
done
