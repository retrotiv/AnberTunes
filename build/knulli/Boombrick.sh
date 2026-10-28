#!/bin/sh
# Knulli lists this script under Ports and runs it to start Boombrick, which
# is in the boombrick folder beside it. Updates replace that folder's files,
# never this one.
exec sh "$(dirname "$0")/boombrick/launch"
