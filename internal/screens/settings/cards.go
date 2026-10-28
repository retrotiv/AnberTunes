package settings

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ujjwal313/boombrick/internal/download"
	"github.com/ujjwal313/boombrick/internal/release"
	"github.com/ujjwal313/boombrick/internal/selfupdate"
	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

// Downloader is yt-dlp as the download queue keeps it. *download.Service is
// one.
type Downloader interface {
	Downloader() download.Downloader
	UpdateDownloader()
}

// Releases finds a repository's latest release. *release.Checker is one.
type Releases interface {
	Check()
	Latest() (release.Release, bool)
}

// Updater stages a newer Boombrick for the next launch. *selfupdate.Updater
// is one.
type Updater interface {
	Stage(r release.Release)
	Status() selfupdate.Status
}

// cardPad is a card's padding above and below; 20 at the sides.
const cardPad = 16

// card is the Downloader or Boombrick card: a title, a line
// about the version, a note in colour when there is news, and a pill when
// there is something to do. Its lines are written when what they say
// changes, not every frame.
type card struct {
	title, line, note string
	noteCol           color.NRGBA
	icon, pill        string // no pill when ""
}

func (c *card) height(u *ui.Context) float32 {
	h := cardPad + u.LineHeight(labelText) + u.LineHeight(valueText)
	if c.note != "" {
		h += u.LineHeight(aboutText)
	}
	if c.pill != "" {
		h += 14 + widgets.PillHeight
	}
	return float32(math.Ceil(float64(h + cardPad)))
}

// setSpotify writes the Spotify account's card: who is signed in, with a
// pill to sign out.
func (c *card) setSpotify(st spotify.Status) {
	c.title, c.note, c.icon, c.pill = "Spotify", "", "", ""
	switch st.State {
	case spotify.SignedOut, spotify.SigningIn:
		c.line = "Not signed in · sign in from the Spotify tab"
		return
	case "":
		c.line = "Checking…"
		return
	default:
		name := st.Name
		if name == "" {
			name = st.User
		}
		c.line = name + " · signed in"
		if st.Premium {
			c.line = name + " · Premium · signed in"
		}
	}
	c.icon, c.pill = "logout", "Sign out"
}

// draw draws the card in box, as tall as height, its pill focused at k.
func (c *card) draw(u *ui.Context, box gfx.Rect, k float32) {
	u.Shape(raster.Shape{W: int(box.W), H: int(box.H), Radius: 14, Kind: raster.Ring, RingOffset: -1, RingWidth: 1}, box,
		theme.PaperA(0.14))
	x, y := box.X+20, box.Y+cardPad
	u.Text(c.title, labelText, x, y, theme.Paper)
	y += u.LineHeight(labelText)
	u.Text(c.line, valueText, x, y, theme.Ink2)
	y += u.LineHeight(valueText)
	if c.note != "" {
		u.Text(c.note, aboutText, x, y, c.noteCol)
		y += u.LineHeight(aboutText)
	}
	if c.pill != "" {
		y += 14
		widgets.PillGlow(u, c.icon, c.pill, x, y, k)
		widgets.Pill(u, c.icon, c.pill, x, y, k)
	}
}

// toolState is what the Downloader card was last written for.
type toolState struct {
	d      download.Downloader
	latest string
	days   int
}

// setTool writes the Downloader card for d, latest (yt-dlp's latest release,
// "" until known) and now.
func (c *card) setTool(d download.Downloader, latest string, now time.Time) {
	c.title = "Downloader"
	installed := d.Version != "" || !d.Installed.IsZero()
	switch {
	case !installed:
		c.line = "Not installed · installs with the first download"
	case d.Version == "":
		c.line = "Installed · " + ago(daysSince(d.Installed, now))
	default:
		c.line = "Version " + d.Version + " · " + ago(daysSince(d.Installed, now))
	}
	c.note, c.noteCol = "", theme.GoldSoft
	switch {
	case d.Updating:
		c.note = "Updating… " + percent(d.Progress)
	case d.Failed:
		c.note, c.noteCol = "Couldn’t update the downloader", theme.Ink2
	case latest != "" && d.Version != "" && release.Newer(latest, d.Version):
		c.note = latest + " is available"
	}
	c.icon, c.pill = "refresh", "Update downloader"
	if !installed {
		c.pill = "Install downloader"
	}
}

// appState is what the Boombrick card was last written for.
type appState struct {
	latest string
	st     selfupdate.Status
}

// setApp writes the Boombrick card for this build's version, the latest
// release ("" until known) and the update's status; canUpdate says whether
// there is an updater to offer it.
func (c *card) setApp(version, latest string, st selfupdate.Status, canUpdate bool) {
	c.title = "AnberTunes"
	switch {
	case version == "dev":
		c.line = "Development build"
	case strings.Contains(version, "-"):
		// A beta is offered the next full release, never another beta, so it
		// can't tell whether it is up to date.
		c.line = "Version " + version + " · beta"
	case latest != "" && !release.Newer(latest, version):
		c.line = "Version " + version + " · up to date"
	default:
		c.line = "Version " + version
	}
	available := latest != "" && release.Newer(latest, version)
	c.note, c.noteCol, c.pill = "", theme.GoldSoft, ""
	switch {
	case st.Staging:
		c.note = "Downloading… " + percent(st.Progress)
	case st.Staged != "":
		c.note = st.Staged + " installs the next time you open AnberTunes"
	case st.Failed:
		c.note, c.noteCol = "Couldn’t download the update", theme.Ink2
	case available:
		c.note = latest + " is available"
	}
	if canUpdate && available && st.Staged != latest {
		c.icon, c.pill = "download", "Update AnberTunes"
	}
}

func daysSince(t, now time.Time) int { return max(int(now.Sub(t)/(24*time.Hour)), 0) }

// ago is how long ago something was updated, in days.
func ago(days int) string {
	switch days {
	case 0:
		return "updated today"
	case 1:
		return "updated yesterday"
	}
	return "updated " + strconv.Itoa(days) + " days ago"
}

func percent(p float32) string { return strconv.Itoa(int(p*100)) + " %" }
