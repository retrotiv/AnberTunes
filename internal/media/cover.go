package media

// Cover names an album's art: the album artist and title, and one of the
// album's files, whose tags and folder are where its picture is looked for.
// A cover with a URL is a picture on the web instead, as YouTube Music's
// results have.
type Cover struct {
	Artist, Album, Path string
	URL                 string
}

// Key identifies the picture; covers with the same key share it. It is
// empty when there is no album or URL to name.
func (c Cover) Key() string {
	if c.URL != "" {
		return c.URL
	}
	if c.Album == "" {
		return ""
	}
	return c.Artist + "\x1f" + c.Album
}
