package helper

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/jfreymuth/vorbis"

	"github.com/ujjwal313/boombrick/internal/spotify"
)

// Spotify's tracks are Ogg Vorbis, at spotify.Rate and in stereo, behind a
// page of Spotify's own.
const (
	trackChannels = 2
	// seekBack is how far before a seek's frame its page is looked for, so
	// the page found starts before it; stepBack how far back a seek tries
	// again from when it still lands after it.
	seekBack = spotify.Rate
	stepBack = 32 << 10
)

// seekTable maps a frame to roughly where in the Vorbis stream its page is,
// as Spotify's page gives it: the byte position, in 1/65536ths of the
// stream's size, at every hundredth of its length. It is read as
// go-librespot's vorbis package reads it.
type seekTable struct {
	frames uint32 // the track's length
	size   uint32 // the stream's size
	at     [100]int32
	ok     bool
}

// seekSteps turn the table's one-byte entries into steps between positions.
var seekSteps = [...]uint16{
	0, 112, 197, 327, 374, 394, 407, 417, 425, 433, 439, 444, 449, 454, 458, 462, 466, 470, 473, 477, 480, 483, 486,
	489, 491, 494, 497, 499, 502, 504, 506, 509, 511, 513, 515, 517, 519, 521, 523, 525, 527, 529, 531, 533, 535, 537,
	538, 540, 542, 544, 545, 547, 549, 550, 552, 554, 555, 557, 558, 560, 562, 563, 565, 566, 568, 569, 571, 572, 574,
	575, 577, 578, 580, 581, 583, 584, 585, 587, 588, 590, 591, 593, 594, 595, 597, 598, 599, 601, 602, 604, 605, 606,
	608, 609, 610, 612, 613, 615, 616, 617, 619, 620, 621, 623, 624, 625, 627, 628, 629, 631, 632, 634, 635, 636, 638,
	639, 640, 642, 643, 644, 646, 647, 649, 650, 651, 653, 654, 655, 657, 658, 660, 661, 662, 664, 665, 667, 668, 669,
	671, 672, 674, 675, 677, 678, 679, 681, 682, 684, 685, 687, 688, 690, 691, 693, 694, 696, 697, 699, 700, 702, 704,
	705, 707, 708, 710, 712, 713, 715, 716, 718, 720, 721, 723, 725, 727, 728, 730, 732, 734, 735, 737, 739, 741, 743,
	745, 747, 748, 750, 752, 754, 756, 758, 760, 763, 765, 767, 769, 771, 773, 776, 778, 780, 782, 785, 787, 790, 792,
	795, 797, 800, 803, 805, 808, 811, 814, 817, 820, 823, 826, 829, 833, 836, 840, 843, 847, 851, 855, 859, 863, 868,
	872, 877, 882, 887, 893, 898, 904, 911, 918, 925, 933, 941, 951, 961, 972, 985, 1000, 1017, 1039, 1067, 1108, 1183,
	1520, 2658, 4666, 8191,
}

// position is roughly where frame's page is, from the Vorbis stream's start.
func (t seekTable) position(frame int64) int64 {
	rel := float32(frame) * 100 / float32(t.frames)
	i := min(max(int32(rel), 1), 99)
	prev, cur := t.at[i-1], t.at[i]
	at := float32(cur-prev)*(rel-float32(i)) + float32(prev)
	return max(int64(at*1.525879e-05*float32(t.size)), 0)
}

// spotifyPage reads the page Spotify puts in front of a track's Vorbis
// stream, whose one packet starts 0x81 and holds the seek table among other
// segments. It returns where the Vorbis stream starts; a file without the
// page starts at 0, with no table.
func spotifyPage(p *pages) (int64, seekTable, error) {
	var t seekTable
	p.at(0)
	pg, err := p.next()
	if err != nil {
		return 0, t, fmt.Errorf("vorbis: no first page: %w", err)
	}
	body := pg.body
	if len(body) == 0 || body[0] != 0x81 {
		return 0, t, nil
	}
	r := bytes.NewReader(body[1:])
	for {
		var n uint16
		if binary.Read(r, binary.LittleEndian, &n) != nil {
			break
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(r, seg); err != nil || n == 0 {
			break
		}
		if seg[0] != 0 || len(seg) < 1+4+4+1+100 {
			continue // not the seek table: gains, and a segment of FFs
		}
		t.frames = binary.LittleEndian.Uint32(seg[1:])
		t.size = binary.LittleEndian.Uint32(seg[5:])
		at := -int32(seekSteps[seg[9]])
		for i, step := range seg[10:110] {
			at += int32(seekSteps[step])
			t.at[i] = at
		}
		t.ok = t.frames > 0
	}
	return p.off, t, nil
}

// vorbisStream is a Spotify track's audio, decoded: a Vorbis stream read
// page by page from a ReaderAt, levelled by gain. Where the frames decoded
// are is told by the pages' granule positions, so a seek lands on its frame.
type vorbisStream struct {
	pages *pages
	start int64 // where the Vorbis stream starts, after Spotify's page
	audio int64 // where its audio starts, after the headers
	table seekTable
	dec   vorbis.Decoder
	gain  float32

	scratch []float32 // a packet decoded
	partial []byte    // a packet the last page left unfinished
	skip    bool      // the next page's continued packet began before a seek: dropped
	out     []float32 // decoded, not yet read, from out[r]
	r       int
	known   bool  // pos is known: a page has ended since the last seek
	pos     int64 // the frame at out[r]
	want    int64 // the first frame to read; those before it are dropped
	last    bool  // the last page has been decoded
	start0  bool  // reading began at the audio's start, at frame 0
	lost    bool  // a seek found no page it can tell the place of
}

// newVorbis reads the headers of the Vorbis stream in r, size bytes long,
// and makes it ready to read from the start.
func newVorbis(r io.ReaderAt, size int64, gain float32) (*vorbisStream, error) {
	t := &vorbisStream{pages: newPages(r, size), gain: gain}
	var err error
	if t.start, t.table, err = spotifyPage(t.pages); err != nil {
		return nil, err
	}
	t.pages.at(t.start)
	var packet []byte
	for !t.dec.HeadersRead() {
		pg, err := t.pages.next()
		if err != nil {
			return nil, fmt.Errorf("vorbis: headers: %w", err)
		}
		for i, n := range pg.lacing {
			packet = append(packet, pg.body[:n]...)
			pg.body = pg.body[n:]
			if n == 255 && i == len(pg.lacing)-1 {
				break // goes on on the next page
			}
			if n < 255 {
				if err := t.dec.ReadHeader(packet); err != nil {
					return nil, err
				}
				packet = packet[:0]
			}
		}
	}
	if t.dec.SampleRate() != spotify.Rate || t.dec.Channels() != trackChannels {
		return nil, fmt.Errorf("vorbis: %d Hz, %d channels", t.dec.SampleRate(), t.dec.Channels())
	}
	// The first audio packet starts a page of its own.
	t.audio = t.pages.off
	t.scratch = make([]float32, t.dec.BufferSize())
	t.restart(t.audio, 0)
	return t, nil
}

// SeekMs makes the next frame read the one at ms; past the end, the next read
// returns io.EOF. It looks for the page a little before it with the seek
// table, steps back while the page found is after it, and decodes on from
// there, dropping the frames before it.
func (t *vorbisStream) SeekMs(ms int64) error {
	want := ms * spotify.Rate / 1000
	off := t.audio
	if want > seekBack && t.table.ok {
		// Past the end, the table points past the file too.
		off = min(max(t.audio, t.start+t.table.position(want-seekBack)), t.pages.end)
	}
	for {
		t.restart(off, want)
		if err := t.locate(); err != nil {
			return err
		}
		if !t.lost && t.pos <= want || off == t.audio {
			return nil
		}
		off = max(t.audio, off-stepBack)
	}
}

// restart makes reading go on from the first page at or after off, wanting
// frame want first.
func (t *vorbisStream) restart(off, want int64) {
	t.dec.Clear()
	t.out, t.r, t.partial = t.out[:0], 0, t.partial[:0]
	t.known, t.pos, t.want, t.last, t.lost = false, 0, want, false, false
	t.start0 = off == t.audio
	t.skip = !t.start0
	if t.start0 {
		t.pages.at(off)
	} else if t.pages.sync(off) != nil {
		t.last, t.lost = true, true
	}
}

// locate decodes until where the frames decoded are is known.
func (t *vorbisStream) locate() error {
	for !t.known && !t.last {
		if err := t.page(); err != nil {
			return err
		}
	}
	return nil
}

// Read reads decoded samples, interleaved, into p; io.EOF after the last.
func (t *vorbisStream) Read(p []float32) (int, error) {
	for t.r == len(t.out) || !t.known {
		if t.last {
			return 0, io.EOF
		}
		if t.r == len(t.out) {
			t.out, t.r = t.out[:0], 0
		}
		if err := t.page(); err != nil {
			return 0, err
		}
	}
	n := copy(p[:len(p)/trackChannels*trackChannels], t.out[t.r:])
	t.r += n
	t.pos += int64(n / trackChannels)
	return n, nil
}

// page decodes the next page's packets and, once one has ended, places what
// was decoded by the page's granule position.
func (t *vorbisStream) page() error {
	pg, err := t.pages.next()
	if errors.Is(err, io.EOF) {
		t.last, t.known = true, true
		return nil
	} else if err != nil {
		return err
	}
	if pg.flags&flagContinued == 0 || t.skip {
		t.partial = t.partial[:0]
	}
	dropping := pg.flags&flagContinued != 0 && t.skip
	ended := false
	for _, n := range pg.lacing {
		if !dropping {
			t.partial = append(t.partial, pg.body[:n]...)
		}
		pg.body = pg.body[n:]
		if n == 255 {
			continue
		}
		if dropping {
			dropping = false
			continue
		}
		ended = true
		t.decode(t.partial)
		t.partial = t.partial[:0]
	}
	t.skip = dropping // the packet dropped goes on on the next page too
	if ended && pg.granule >= 0 {
		t.place(pg.granule, pg.flags&flagLast != 0)
	}
	if pg.flags&flagLast != 0 {
		t.last, t.known = true, true
	}
	return nil
}

// decode decodes a packet onto out, levelled. A damaged packet is left out,
// as libvorbis leaves it.
func (t *vorbisStream) decode(packet []byte) {
	s, err := t.dec.DecodeInto(packet, t.scratch)
	if err != nil {
		return
	}
	for _, v := range s {
		t.out = append(t.out, v*t.gain)
	}
}

// place sets where the frames waiting are, from the granule position at the
// end of the page just decoded, and drops those before want. The last page's
// granule may also cut the stream short of its last packet's end, so it
// can't place frames by itself: they start at 0 when the page is the first
// read, and are lost when a seek found it.
func (t *vorbisStream) place(granule int64, last bool) {
	frames := int64(len(t.out)-t.r) / trackChannels
	if !t.known {
		t.pos, t.known = granule-frames, true
		if last && t.start0 {
			t.pos = 0
		} else if last {
			t.lost = true
		}
	}
	if last && t.pos+frames > granule {
		t.out = t.out[:t.r+int(max(granule-t.pos, 0))*trackChannels]
	}
	if t.pos < t.want {
		drop := min(t.want-t.pos, int64(len(t.out)-t.r)/trackChannels)
		t.r += int(drop) * trackChannels
		t.pos += drop
	}
}
