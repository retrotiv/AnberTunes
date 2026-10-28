// Package download keeps the queue of YouTube Music songs to download: it
// runs yt-dlp for batches of them, remuxes and tags what lands, and files it
// in the Music folder.
package download

import "strings"

// Failure is why a download failed, which decides what the failure dialog
// offers.
type Failure uint8

const (
	NoFailure Failure = iota
	// NeedsUpdate: yt-dlp couldn't read YouTube's pages or player, which a
	// newer yt-dlp usually fixes.
	NeedsUpdate
	// Network: the connection failed or YouTube's servers did.
	Network
	// Unavailable: YouTube won't give the track: removed, private, blocked
	// in this country, for members only or for signed-in adults.
	Unavailable
	// Other: anything else, such as a file that couldn't be written.
	Other
)

// classes are what yt-dlp's messages say for each kind, lower-cased,
// checked in this order: "sign in to confirm your age" is a track withheld,
// "sign in to confirm you're not a bot" a check a newer yt-dlp gets past.
var classes = []struct {
	f    Failure
	says []string
}{
	// YouTube's rate limit is reported as "Video unavailable ... try again
	// later", so it is checked before the tracks withheld.
	{Network, []string{"try again later", "rate-limit", "rate limit", "http error 429", "too many requests"}},
	{Unavailable, []string{"video unavailable", "private video", "has been removed", "not available in your country",
		"not made this video available", "members-only", "channel's members", "account associated with this video has been terminated",
		"no longer available", "not available from your location", "unable to download webpage: http error 404", "copyright",
		"confirm your age", "inappropriate for some users"}},
	{Network, []string{"unable to download webpage", "timed out", "connection reset", "connection refused",
		"temporary failure in name resolution", "name or service not known", "network is unreachable", "no route to host",
		"remote end closed connection", "connection aborted", "eof occurred", "http error 5", "ssl:"}},
	{NeedsUpdate, []string{"signature", "nsig", "unable to extract", "requested format is not available",
		"sign in to confirm you", "http error 403", "http error 400", "please report this issue", "javascript", "js runtime", "challenge"}},
}

// Classify sorts one of yt-dlp's messages.
func Classify(msg string) Failure {
	m := strings.ToLower(msg)
	for _, c := range classes {
		for _, s := range c.says {
			if strings.Contains(m, s) {
				return c.f
			}
		}
	}
	return Other
}
