#!/usr/bin/env bash
# Fetches the fonts Boombrick bundles from google/fonts, pinned to a fixed
# commit: Outfit for headings and Figtree for everything else, with Noto Sans
# for the letters they lack: Cyrillic, Greek, Vietnamese and the rest of
# European Latin. All come as variable fonts and are cut into the static
# weights the app uses with fonttools, which scripts/setup-wsl.sh installs;
# Noto Sans is cut down to those letters too. The folder is emptied first:
# every .ttf in it is compiled into the app.
set -euo pipefail

out="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/assets/fonts"
ft="$HOME/.venvs/fonttools/bin/fonttools"
gf=https://raw.githubusercontent.com/google/fonts/47d54ca1ebb14fd864695f1d39dc734f39017c33/ofl
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"
rm -f "$out"/*.ttf "$out"/OFL-*.txt

curl -fsSL -o "$tmp/outfit.ttf"         "$gf/outfit/Outfit%5Bwght%5D.ttf"
curl -fsSL -o "$tmp/figtree.ttf"        "$gf/figtree/Figtree%5Bwght%5D.ttf"
curl -fsSL -o "$tmp/figtree-italic.ttf" "$gf/figtree/Figtree-Italic%5Bwght%5D.ttf"
curl -fsSL -o "$tmp/noto.ttf"           "$gf/notosans/NotoSans%5Bwdth,wght%5D.ttf"
curl -fsSL -o "$out/OFL-Outfit.txt"     "$gf/outfit/OFL.txt"
curl -fsSL -o "$out/OFL-Figtree.txt"    "$gf/figtree/OFL.txt"
curl -fsSL -o "$out/OFL-NotoSans.txt"   "$gf/notosans/OFL.txt"

instance() { "$ft" varLib.instancer --no-recalc-timestamp "$tmp/$1" "wght=$2" -o "$out/$3"; }
instance outfit.ttf         400 Outfit-Regular.ttf
instance outfit.ttf         600 Outfit-SemiBold.ttf
instance figtree.ttf        400 Figtree-Regular.ttf
instance figtree.ttf        600 Figtree-SemiBold.ttf
instance figtree-italic.ttf 400 Figtree-Italic.ttf

# Noto Sans keeps ASCII, so that a letter with a combining accent the other
# fonts can't place moves to it whole; Latin-1 and Extended-A; the letters of
# Extended-B in use (Romanian, Vietnamese, pinyin) and the modifier
# apostrophe; combining accents; Greek; Cyrillic; Vietnamese; and the
# punctuation, currency and letterlike blocks.
noto="U+0020-007E,U+00A0-017F,U+0192,U+01A0-01A1,U+01AF-01B0,U+01CD-01DC,U+0218-021B,U+02BC,U+02C6-02C7"
noto+=",U+02D8-02DD,U+0300-036F,U+0370-03FF,U+0400-052F,U+1E00-1EFF,U+2000-206F,U+20A0-20CF,U+2100-214F"
fallback() {
  "$ft" varLib.instancer --no-recalc-timestamp "$tmp/noto.ttf" "wght=$1" wdth=100 -o "$tmp/$2"
  "$ft" subset "$tmp/$2" --unicodes="$noto" --output-file="$out/$2" \
    --layout-features=ccmp,locl,mark,mkmk,kern,liga,calt,clig,rlig,rvrn,tnum
}
fallback 400 NotoSans-Regular.ttf
fallback 600 NotoSans-SemiBold.ttf

ls -l "$out"
