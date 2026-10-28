package app

import (
	"cmp"
	"time"

	"github.com/ujjwal313/boombrick/internal/download"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/screens/downloads"
	settingsscreen "github.com/ujjwal313/boombrick/internal/screens/settings"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// Downloads is what the app needs from the download queue.
// *download.Service is one.
type Downloads interface {
	downloads.Source
	ytDownloads
	settingsscreen.Downloader // the Downloader card
	Outcomes() []download.Outcome
	Ready() <-chan struct{}
	Busy() bool
	Note() string
	Version() uint64
}

// ytDownloads is what playing and downloading YouTube Music songs needs from
// the download queue.
type ytDownloads interface {
	State(videoID string) (media.DownloadState, float32)
	Get(songs []ytmusic.Song)
	First(videoID string)
	Path(videoID string) (string, bool)
	Paused() bool
}

// ytPlayer is what it needs from the player.
type ytPlayer interface {
	SetShuffle(on bool)
	Play(tracks []player.Track, start int)
	Enqueue(tracks []player.Track)
}

// ytActions plays and downloads YouTube Music songs for the YouTube screens:
// a saved song plays from its file, any other as a pending track while it
// downloads.
type ytActions struct {
	dl    ytDownloads
	p     ytPlayer
	toast func(msg string)
}

func (y ytActions) State(videoID string) (media.DownloadState, float32) { return y.dl.State(videoID) }

func (y ytActions) Get(songs []ytmusic.Song) { y.dl.Get(songs) }

// Play plays songs from start, in order, as the Library's Play does; the one
// starting goes first in the download queue when it isn't saved. With
// downloads paused it says so, rather than resuming them.
func (y ytActions) Play(songs []ytmusic.Song, start int) {
	if len(songs) == 0 {
		return
	}
	start = min(max(start, 0), len(songs)-1)
	tracks := y.tracks(songs)
	y.dl.Get(songs)
	if tracks[start].Pending() {
		y.dl.First(songs[start].VideoID)
		if y.dl.Paused() {
			y.toast("Downloads are paused")
		}
	}
	y.p.SetShuffle(false)
	y.p.Play(tracks, start)
}

func (y ytActions) Enqueue(songs []ytmusic.Song) {
	y.dl.Get(songs)
	y.p.Enqueue(y.tracks(songs))
}

// tracks are songs as queue entries, with their files when saved.
func (y ytActions) tracks(songs []ytmusic.Song) []player.Track {
	out := make([]player.Track, len(songs))
	for i, s := range songs {
		out[i] = player.Track{Title: s.Title, Artist: s.Artist, AlbumArtist: cmp.Or(s.AlbumArtist, s.Artist),
			Album: s.Album, Duration: s.Duration, VideoID: s.VideoID}
		out[i].Path, _ = y.dl.Path(s.VideoID)
	}
	return out
}

// settler is what settling downloads needs from the player.
type settler interface {
	Resolve(videoID, path string)
	Drop(videoID string) int
}

// settle gives the player the downloads that ended: their files to play, or
// for failures and removals, their songs taken out of the queue. It reports
// whether a file landed and how many queued songs were dropped for failing;
// the Downloads tab says itself what it removed.
func settle(p settler, outcomes []download.Outcome) (landed bool, dropped int) {
	for _, o := range outcomes {
		switch {
		case o.Path != "":
			p.Resolve(o.VideoID, o.Path)
			landed = true
		case o.Removed:
			p.Drop(o.VideoID)
		default:
			dropped += p.Drop(o.VideoID)
		}
	}
	return landed, dropped
}

// waitFor moves the download playback waits on to the front of the queue,
// or hands the player the file if it has already
// landed. A song the queue has forgotten, as when a power cut lost its row,
// is asked for again.
func waitFor(dl ytDownloads, p settler, t player.Track) {
	if path, ok := dl.Path(t.VideoID); ok {
		p.Resolve(t.VideoID, path)
		return
	}
	if st, _ := dl.State(t.VideoID); st == media.Available {
		dl.Get([]ytmusic.Song{{VideoID: t.VideoID, Title: t.Title, Artist: t.Artist, AlbumArtist: t.AlbumArtist,
			Album: t.Album, Duration: t.Duration}})
	}
	dl.First(t.VideoID)
}

// reloadEvery is how often songs landing reload the library at most while the
// queue is busy, as a scan's progress does.
var reloadEvery = 10 * time.Second

// followDownloads settles the downloads that end and asks for the ones
// playback waits on, in turn, off the UI thread: Resolve and Drop may open
// the next file. It tells poll what to show: songs that landed reload the
// library at once when the queue is idle, and otherwise at most every
// reloadEvery. It ends when the app or the queue closes.
func (a *App) followDownloads(dl Downloads, p settler) {
	var timer *time.Timer
	var due <-chan time.Time
	var last time.Time // when songs that landed last reloaded the library
	pending := false   // songs have landed since
	reload := func() {
		if timer != nil {
			timer.Stop()
			timer, due = nil, nil
		}
		pending, last = false, time.Now()
		a.landed.Store(true)
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-a.stop:
			return
		case _, ok := <-dl.Ready():
			if !ok {
				return
			}
			landed, dropped := settle(p, dl.Outcomes())
			pending = pending || landed
			switch {
			case !pending:
			case !dl.Busy() || time.Since(last) >= reloadEvery:
				reload()
			case timer == nil:
				timer = time.NewTimer(reloadEvery - time.Since(last))
				due = timer.C
			}
			if dropped > 0 {
				a.skipped.Store(true)
			}
			a.downloaded.Store(true)
			a.wake()
		case <-due:
			timer, due = nil, nil
			reload()
			a.wake()
		case t := <-a.waits:
			waitFor(dl, p, t)
		}
	}
}

// askFirst hands followDownloads the track playback now waits on, in place of
// any it hasn't taken yet. Only poll sends, so there is room after the drain.
func (a *App) askFirst(t player.Track) {
	select {
	case <-a.waits:
	default:
	}
	a.waits <- t
}
