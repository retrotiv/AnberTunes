module github.com/ujjwal313/boombrick

go 1.25.0

toolchain go1.27.2

require (
	github.com/devgianlu/go-librespot v0.10.5
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/go-text/render v0.2.1
	github.com/go-text/typesetting v0.3.5
	github.com/godbus/dbus/v5 v5.2.0
	github.com/jfreymuth/vorbis v1.0.2
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	github.com/veandco/go-sdl2 v0.4.40
	golang.org/x/crypto/x509roots/fallback v0.0.0-20260721213823-31914c699bfc
	golang.org/x/image v0.45.0
	golang.org/x/text v0.41.0
	google.golang.org/protobuf v1.36.11
	rsc.io/qr v0.2.0
)

require (
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/devgianlu/shannon v0.0.0-20230613115856-82ec90b7fa7e // indirect
	github.com/jfreymuth/pulse v0.1.2-0.20241102120944-4ffb35054b53 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/xlab/vorbis-go v0.0.0-20210911202351-b5b85f1ec645 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/exp v0.0.0-20251209150349-8475f28825e9 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/oauth2 v0.34.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/xlab/vorbis-go => ./third_party/novorbis

replace github.com/srwiley/oksvg => ./third_party/oksvg

replace github.com/dhowden/tag => ./third_party/tag
