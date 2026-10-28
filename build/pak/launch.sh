#!/bin/sh
# NextUI starts tools by running this script.
PAK_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$PAK_DIR" || exit 1

# An update the app staged in .update is moved into place by renames, this
# script last, and the new script is run. One cut short by a power cut is
# finished on the next launch; a staged folder without "ready" is incomplete
# and is deleted.
if [ -f .update/ready ] && [ -z "$BOOMBRICK_UPDATED" ]; then
	(
		cd .update/pak || exit 1
		find . -type d | while read -r d; do mkdir -p "$PAK_DIR/$d"; done
		find . -type f ! -path ./launch.sh | while read -r f; do mv -f "$f" "$PAK_DIR/$f"; done
	)
	[ -f .update/pak/launch.sh ] && mv -f .update/pak/launch.sh launch.sh
	sync
	# Anything left means a rename failed; the next launch tries again.
	[ -z "$(find .update/pak -type f)" ] && rm -rf .update
	export BOOMBRICK_UPDATED=1
	exec sh ./launch.sh
fi
[ -f .update/ready ] || rm -rf .update

export LD_LIBRARY_PATH="$PAK_DIR/lib:$LD_LIBRARY_PATH"

LOGS_PATH="${LOGS_PATH:-$USERDATA_PATH/logs}"
mkdir -p "$LOGS_PATH"

# Restores NextUI's brightness and volume once the app has opened the display.
syncsettings.elf &

# The app writes its own log; this file only catches crashes before logging starts.
exec ./boombrick > "$LOGS_PATH/Boombrick.crash.txt" 2>&1
