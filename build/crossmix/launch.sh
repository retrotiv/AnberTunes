#!/bin/sh
# CrossMix starts apps by running this script.
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

export LD_LIBRARY_PATH="$PAK_DIR/lib:/usr/trimui/lib:$LD_LIBRARY_PATH"
export BOOMBRICK_PLATFORM=crossmix
# Tests give their own /tmp and poweroff.
TMP_DIR="${BOOMBRICK_TMP:-/tmp}"
POWEROFF="${BOOMBRICK_POWEROFF:-poweroff}"
mkdir -p logs

# Keeps CrossMix from sleeping while Boombrick is open, as its own music
# player does. A power-off flag from a run cut short isn't this run's.
echo 1 > "$TMP_DIR/stay_awake"
rm -f "$TMP_DIR/boombrick-poweroff"

# The app writes its own log; this file only catches crashes before logging
# starts. A signal to this script goes on to Boombrick, which saves the queue
# as it quits, as when NextUI's launcher hands over to it with exec.
./boombrick > logs/Boombrick.crash.txt 2>&1 &
pid=$!
trap 'kill -TERM "$pid" 2>/dev/null' TERM INT HUP
wait "$pid"
wait "$pid" 2>/dev/null # again, when a signal cut the first short
rm -f "$TMP_DIR/stay_awake"

# Holding Power asks for the device to be powered off once Boombrick has quit.
if [ -f "$TMP_DIR/boombrick-poweroff" ]; then
	rm -f "$TMP_DIR/boombrick-poweroff"
	sync
	"$POWEROFF"
fi
