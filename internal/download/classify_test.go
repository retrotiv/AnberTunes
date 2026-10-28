package download

import "testing"

func TestClassify(t *testing.T) {
	cases := map[string]Failure{
		"ERROR: [youtube] abc: Video unavailable. This video is no longer available because the YouTube account associated with this video has been terminated.":          Unavailable,
		"ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video":                                                                        Unavailable,
		"ERROR: [youtube] abc: The uploader has not made this video available in your country":                                                                            Unavailable,
		"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.":                                                              Unavailable,
		"ERROR: [youtube] abc: Join this channel to get access to members-only content like this video":                                                                   Unavailable,
		"ERROR: [youtube] abc: Unable to download webpage: <urlopen error [Errno -3] Temporary failure in name resolution>":                                               Network,
		"ERROR: [download] Got error: The read operation timed out":                                                                                                       Network,
		"ERROR: unable to download video data: HTTP Error 503: Service Unavailable":                                                                                       Network,
		"ERROR: unable to download video data: HTTP Error 403: Forbidden":                                                                                                 NeedsUpdate,
		"ERROR: [youtube] abc: Requested format is not available. Use --list-formats for a list of available formats":                                                     NeedsUpdate,
		"ERROR: [youtube] abc: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.":                                      NeedsUpdate,
		"WARNING: [youtube] abc: nsig extraction failed: Some formats may be missing":                                                                                     NeedsUpdate,
		"ERROR: unable to write data: [Errno 28] No space left on device":                                                                                                 Other,
		"ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later. The current session has been rate-limited by YouTube for up to an hour.": Network,
		"ERROR: [youtube] abc: This content isn't available, try again later.":                                                                                            Network,
		"ERROR: unable to download video data: HTTP Error 429: Too Many Requests":                                                                                         Network,
		"ERROR: [youtube] abc: Unable to download API page: HTTP Error 400: Bad Request":                                                                                  NeedsUpdate,
		"ERROR: [youtube] abc: Unable to download webpage: HTTP Error 404: Not Found":                                                                                     Unavailable,
		"ERROR: unable to download video data: HTTP Error 404: Not Found":                                                                                                 Other,
		"ERROR: [youtube] abc: The uploader has not made this video available from your location. This video is not available from your location":                         Unavailable,
		"": Other,
	}
	for msg, want := range cases {
		if got := Classify(msg); got != want {
			t.Errorf("Classify(%.60q) = %d, want %d", msg, got, want)
		}
	}
}
