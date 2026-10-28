package remux

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// EBML for test files. Sizes are written in the eight-byte form, or as
// unknown, which a reader must take as well as the shortest form.

func ebmlID(id uint32) []byte {
	b := binary.BigEndian.AppendUint32(nil, id)
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

func el(id uint32, body ...[]byte) []byte {
	all := bytes.Join(body, nil)
	b := append(ebmlID(id), 0x01)
	b = append(b, binary.BigEndian.AppendUint64(nil, uint64(len(all)))[1:]...)
	return append(b, all...)
}

// unsized is a master of unknown size, as live streams write them.
func unsized(id uint32, body ...[]byte) []byte {
	return append(append(ebmlID(id), 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff), bytes.Join(body, nil)...)
}

func uintEl(id uint32, v uint64) []byte { return el(id, binary.BigEndian.AppendUint64(nil, v)) }

func opusHead(preSkip int) []byte {
	h := append([]byte("OpusHead"), 1, 2)
	h = binary.LittleEndian.AppendUint16(h, uint16(preSkip))
	h = binary.LittleEndian.AppendUint32(h, 48000)
	return append(h, 0, 0, 0) // no gain, channel mapping 0
}

// packet is an Opus packet with TOC byte toc and n bytes after it.
func packet(toc byte, n int) []byte {
	p := []byte{toc}
	for i := range n {
		p = append(p, byte(i))
	}
	return p
}

// testPackets last 960, 960, 1920, 2880, 960 and 960 samples: 8640 in all.
var testPackets = [][]byte{
	packet(0xF8, 40),   // CELT 20 ms, one frame
	packet(0xF8, 3),    //
	packet(0xF9, 60),   // CELT 20 ms, two frames
	{0x0B, 0x83, 1, 2}, // SILK 20 ms, code 3: three frames
	packet(0x6C, 20),   // hybrid 20 ms
	packet(0xF8, 300),  // more than one lacing value
}

const testSamples = 8640

// testWebM builds a file as YouTube's are: an Opus track (after a video
// track, to be passed over), the packets in two clusters, the last in a
// block group with discardNs of padding. With unknown set, the segment and
// clusters have unknown sizes.
func testWebM(packets [][]byte, discardNs uint64, unknown bool) []byte {
	master := el
	if unknown {
		master = unsized
	}
	tracks := el(idTracks,
		el(idTrackEntry, uintEl(idTrackNumber, 2), el(idCodecID, []byte("V_VP9"))),
		el(idTrackEntry, uintEl(idTrackNumber, 1), el(idCodecID, []byte("A_OPUS")), el(idCodecPrivate, opusHead(312)),
			uintEl(0x56AA, 6500000)))
	half := len(packets) / 2
	first := [][]byte{uintEl(0xE7, 0), el(idSimpleBlock, []byte{0x82, 0, 0, 0x80}, []byte{9, 9})} // a video block
	for _, p := range packets[:half] {
		first = append(first, el(idSimpleBlock, []byte{0x81, 0, 0, 0x80}, p))
	}
	second := [][]byte{uintEl(0xE7, 1000)}
	for i, p := range packets[half:] {
		if i == len(packets)-half-1 {
			second = append(second, el(idBlockGroup, uintEl(idDiscardPadding, discardNs), el(idBlock, []byte{0x81, 0, 0, 0}, p)))
			continue
		}
		second = append(second, el(idSimpleBlock, []byte{0x81, 0, 0, 0x80}, p))
	}
	segment := master(idSegment,
		el(0x1549A966, uintEl(0x2AD7B1, 1000000)), // Info
		tracks,
		master(idCluster, first...),
		master(idCluster, second...),
		el(0x1C53BB6B, el(0xBB, uintEl(0xB3, 0)))) // Cues
	return append(el(idEBML, el(0x4282, []byte("webm"))), segment...)
}

type oggPage struct {
	flags   byte
	granule int64
	seq     uint32
	segs    []byte
	body    []byte
}

// readPages splits an Ogg stream into pages, checking each page's CRC.
func readPages(t *testing.T, b []byte) []oggPage {
	t.Helper()
	var pages []oggPage
	for len(b) > 0 {
		if len(b) < 27 || string(b[:4]) != "OggS" {
			t.Fatalf("no page where page %d should be", len(pages))
		}
		n := int(b[26])
		segs := b[27 : 27+n]
		size := 27 + n
		for _, s := range segs {
			size += int(s)
		}
		page := b[:size]
		check := slices.Clone(page)
		clear(check[22:26])
		if got, want := binary.LittleEndian.Uint32(page[22:]), crc(0, check); got != want {
			t.Fatalf("page %d: CRC %08x, want %08x", len(pages), got, want)
		}
		pages = append(pages, oggPage{flags: page[5], granule: int64(binary.LittleEndian.Uint64(page[6:])),
			seq: binary.LittleEndian.Uint32(page[18:]), segs: segs, body: page[27+n:]})
		b = b[size:]
	}
	return pages
}

// packetsOf joins pages' packets, noting the page each ends on.
func packetsOf(pages []oggPage) (packets [][]byte, ends []int) {
	var cur []byte
	for i, pg := range pages {
		off := 0
		for _, s := range pg.segs {
			cur = append(cur, pg.body[off:off+int(s)]...)
			off += int(s)
			if s < 255 {
				packets, ends, cur = append(packets, cur), append(ends, i), nil
			}
		}
	}
	return packets, ends
}

func readComments(t *testing.T, p []byte) []string {
	t.Helper()
	rest, ok := bytes.CutPrefix(p, []byte("OpusTags"))
	if !ok {
		t.Fatalf("second packet %q", p[:min(len(p), 8)])
	}
	str := func() string {
		n := binary.LittleEndian.Uint32(rest)
		s := string(rest[4 : 4+n])
		rest = rest[4+n:]
		return s
	}
	if v := str(); v != "Boombrick" {
		t.Fatalf("vendor %q", v)
	}
	n := binary.LittleEndian.Uint32(rest)
	rest = rest[4:]
	var out []string
	for range n {
		out = append(out, str())
	}
	return out
}

func readPicture(t *testing.T, comment string) Picture {
	t.Helper()
	b64, ok := strings.CutPrefix(comment, "METADATA_BLOCK_PICTURE=")
	if !ok {
		t.Fatalf("comment %.40q", comment)
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	u32 := func() int {
		v := binary.BigEndian.Uint32(b)
		b = b[4:]
		return int(v)
	}
	str := func() string {
		n := u32()
		s := string(b[:n])
		b = b[n:]
		return s
	}
	if typ := u32(); typ != 3 {
		t.Fatalf("picture type %d", typ)
	}
	p := Picture{MIME: str()}
	if desc := str(); desc != "" {
		t.Fatalf("description %q", desc)
	}
	p.Width, p.Height = u32(), u32()
	if depth, colours := u32(), u32(); depth != 24 || colours != 0 {
		t.Fatalf("depth %d, colours %d", depth, colours)
	}
	n := u32()
	p.Data = b[:n]
	return p
}

var testTags = Tags{Title: "Night Drive Theory", Artist: "Lumen Coast", Album: "Night Drive Theory", AlbumArtist: "Lumen Coast",
	Track: 1, Year: 2023, Lyrics: "[00:01.00]Headlights", Extra: [][2]string{{"BOOMBRICK_YOUTUBE_ID", "v1"}},
	Picture: &Picture{MIME: "image/jpeg", Width: 2, Height: 2, Data: []byte("a picture")}}

func TestRemux(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		var out bytes.Buffer
		if err := remux(bytes.NewReader(testWebM(testPackets, 3_000_000, unknown)), &out, testTags); err != nil {
			t.Fatal(err)
		}
		pages := readPages(t, out.Bytes())
		packets, ends := packetsOf(pages)
		if len(packets) != 2+len(testPackets) {
			t.Fatalf("%d packets", len(packets))
		}
		// OpusHead alone on the first page; OpusTags ending its own.
		if !bytes.Equal(packets[0], opusHead(312)) || ends[0] != 0 || len(pages[0].segs) != 1 ||
			pages[0].flags != 0x02 || pages[0].granule != 0 {
			t.Fatalf("first page %+v", pages[0])
		}
		if ends[1] != 1 || pages[1].granule != 0 || ends[2] == 1 {
			t.Fatal("OpusTags doesn't have its page to itself")
		}
		comments := readComments(t, packets[1])
		want := []string{"TITLE=Night Drive Theory", "ARTIST=Lumen Coast", "ALBUM=Night Drive Theory",
			"ALBUMARTIST=Lumen Coast", "TRACKNUMBER=1", "DATE=2023", "LYRICS=[00:01.00]Headlights", "BOOMBRICK_YOUTUBE_ID=v1"}
		if len(comments) != len(want)+1 || !slices.Equal(comments[:len(want)], want) {
			t.Fatalf("comments %q", comments)
		}
		if p := readPicture(t, comments[len(want)]); p.MIME != "image/jpeg" || p.Width != 2 || p.Height != 2 ||
			string(p.Data) != "a picture" {
			t.Fatalf("picture %+v", p)
		}
		// The audio, unchanged, with granules counting the samples up to each
		// page's last packet; the last page ends the stream, trimmed by the
		// 3 ms (144 samples) of padding.
		var total int64
		for i, p := range packets[2:] {
			if !bytes.Equal(p, testPackets[i]) {
				t.Fatalf("packet %d changed", i)
			}
			total += int64(samples(p))
			last := i == len(testPackets)-1
			if !last && ends[i+3] == ends[i+2] {
				continue // another packet ends on this page
			}
			want := total
			if last {
				want -= 144
			}
			if pg := pages[ends[i+2]]; pg.granule != want {
				t.Fatalf("page %d: granule %d, want %d", ends[i+2], pg.granule, want)
			}
		}
		if last := pages[len(pages)-1]; last.flags != 0x04 || last.granule != testSamples-144 {
			t.Fatalf("last page %+v", last)
		}
		for i, pg := range pages {
			if pg.seq != uint32(i) {
				t.Fatalf("page %d numbered %d", i, pg.seq)
			}
		}
	}
}

// A cover too big for one page carries OpusTags over several.
func TestALargeCoverSpansPages(t *testing.T) {
	tags := testTags
	tags.Picture = &Picture{MIME: "image/jpeg", Width: 544, Height: 544, Data: bytes.Repeat([]byte{0xAB}, 100_000)}
	var out bytes.Buffer
	if err := remux(bytes.NewReader(testWebM(testPackets, 0, false)), &out, tags); err != nil {
		t.Fatal(err)
	}
	pages := readPages(t, out.Bytes())
	packets, ends := packetsOf(pages)
	if ends[1] < 2 || pages[2].flags != 0x01 || pages[1].flags != 0 {
		t.Fatalf("OpusTags ends on page %d; flags %x, %x", ends[1], pages[1].flags, pages[2].flags)
	}
	// A page where no packet ends has granule -1; the one that ends OpusTags, 0.
	for i, pg := range pages[1 : ends[1]+1] {
		want := int64(-1)
		if i == ends[1]-1 {
			want = 0
		}
		if pg.granule != want {
			t.Fatalf("header page %d: granule %d, want %d", i+1, pg.granule, want)
		}
	}
	comments := readComments(t, packets[1])
	if p := readPicture(t, comments[len(comments)-1]); len(p.Data) != 100_000 {
		t.Fatalf("picture of %d bytes", len(p.Data))
	}
	if last := pages[len(pages)-1]; last.granule != testSamples {
		t.Fatalf("no padding, yet the end was trimmed to %d", last.granule)
	}
}

func TestNotWhatItShouldBe(t *testing.T) {
	vorbis := bytes.Replace(testWebM(testPackets, 0, false), []byte("A_OPUS"), []byte("A_VORB"), 1)
	if err := remux(bytes.NewReader(vorbis), &bytes.Buffer{}, Tags{}); !errors.Is(err, ErrNotOpus) {
		t.Errorf("no Opus track: %v", err)
	}
	laced := el(idEBML, el(0x4282, []byte("webm")))
	laced = append(laced, el(idSegment,
		el(idTracks, el(idTrackEntry, uintEl(idTrackNumber, 1), el(idCodecID, []byte("A_OPUS")), el(idCodecPrivate, opusHead(312)))),
		el(idCluster, el(idSimpleBlock, []byte{0x81, 0, 0, 0x82}, packet(0xF8, 4))))...)
	if err := remux(bytes.NewReader(laced), &bytes.Buffer{}, Tags{}); !errors.Is(err, errLaced) {
		t.Errorf("laced: %v", err)
	}
	file := testWebM(testPackets, 0, false)
	for _, n := range []int{0, 3, 40, len(file) / 2, len(file) - 1} {
		if err := remux(bytes.NewReader(file[:n]), &bytes.Buffer{}, Tags{}); err == nil {
			t.Errorf("cut at %d bytes: no error", n)
		}
	}
	if err := remux(strings.NewReader("RIFF....WAVEfmt "), &bytes.Buffer{}, Tags{}); err == nil {
		t.Error("a WAV file remuxed")
	}
}

func TestSamples(t *testing.T) {
	cases := map[string]struct {
		p    []byte
		want int
	}{
		"SILK 10 ms":           {[]byte{0x00}, 480},
		"SILK 60 ms":           {[]byte{0x18}, 2880},
		"hybrid 10 ms":         {[]byte{0x60}, 480},
		"CELT 2.5 ms":          {[]byte{0x80}, 120},
		"CELT 20 ms, 2 frames": {[]byte{0xFA}, 1920},
		"code 3, 6 frames":     {[]byte{0xFB, 0x06}, 5760},
		"code 3, no count":     {[]byte{0xFB}, 0},
		"code 3, over 120 ms":  {[]byte{0xFB, 0x07}, 0},
		"code 3, 120 ms":       {[]byte{0xFB, 0x06}, 5760},
		"empty":                {nil, 0},
	}
	for name, c := range cases {
		if got := samples(c.p); got != c.want {
			t.Errorf("%s: %d samples, want %d", name, got, c.want)
		}
	}
}

// Ogg's CRC is CRC-32/POSIX before its final inversion.
func TestCRC(t *testing.T) {
	if got := crc(0, []byte("123456789")); got != ^uint32(0x765E7680) {
		t.Fatalf("crc = %08x", got)
	}
}

// opusWebM is a file of the test track whose first cluster holds blocks, in
// the order given, and which is cut short of nothing.
func opusWebM(blocks ...[]byte) []byte {
	tracks := el(idTracks,
		el(idTrackEntry, uintEl(idTrackNumber, 2), el(idCodecID, []byte("V_VP9"))),
		el(idTrackEntry, uintEl(idTrackNumber, 1), el(idCodecID, []byte("A_OPUS")), el(idCodecPrivate, opusHead(312))))
	segment := el(idSegment, tracks, el(idCluster, append([][]byte{uintEl(0xE7, 0)}, blocks...)...))
	return append(el(idEBML, el(0x4282, []byte("webm"))), segment...)
}

func audioBlock(p []byte) []byte { return el(idSimpleBlock, []byte{0x81, 0, 0, 0x80}, p) }

// grouped is p in a block group of track, with its padding given before the
// block or after it.
func grouped(track byte, p []byte, padding []byte, before bool) []byte {
	block := el(idBlock, []byte{0x80 | track, 0, 0, 0}, p)
	if before {
		return el(idBlockGroup, padding, block)
	}
	return el(idBlockGroup, block, padding)
}

func lastGranule(t *testing.T, file []byte) int64 {
	t.Helper()
	var out bytes.Buffer
	if err := remux(bytes.NewReader(file), &out, Tags{}); err != nil {
		t.Fatal(err)
	}
	pages := readPages(t, out.Bytes())
	return pages[len(pages)-1].granule
}

// Padding belongs to its own block, so only the last Opus block's counts.
func TestPaddingBelongsToTheLastOpusBlock(t *testing.T) {
	two := packet(0xF8, 10) // 960 samples each; 1920 in all
	ms3 := uintEl(idDiscardPadding, 3_000_000)
	cases := []struct {
		name string
		file []byte
		want int64
	}{
		{"after its block", opusWebM(audioBlock(two), grouped(1, two, ms3, false)), 1920 - 144},
		{"before its block", opusWebM(audioBlock(two), grouped(1, two, ms3, true)), 1920 - 144},
		{"a video block's", opusWebM(audioBlock(two), audioBlock(two), grouped(2, []byte{9}, ms3, false)), 1920},
		{"an earlier block's", opusWebM(grouped(1, two, ms3, false), audioBlock(two)), 1920},
		{"an earlier group's", opusWebM(grouped(1, two, ms3, false), grouped(1, two, nil, false)), 1920},
		{"none", opusWebM(audioBlock(two), audioBlock(two)), 1920},
		{"empty", opusWebM(audioBlock(two), grouped(1, two, el(idDiscardPadding), false)), 1920},
	}
	for _, c := range cases {
		if got := lastGranule(t, c.file); got != c.want {
			t.Errorf("%s: last granule %d, want %d", c.name, got, c.want)
		}
	}
}

// Padding that is negative or longer than the last page can't push the last
// granule above what was decoded or below the page before.
func TestPaddingIsBounded(t *testing.T) {
	one := packet(0xF8, 0)
	var many [][]byte
	for range 300 {
		many = append(many, one)
	}
	for _, ns := range []uint64{1 << 62, 1<<63 - 1, 1 << 40, 1<<64 - 5_000_000} {
		var out bytes.Buffer
		if err := remux(bytes.NewReader(testWebM(many, ns, false)), &out, Tags{}); err != nil {
			t.Fatal(err)
		}
		pages := readPages(t, out.Bytes())
		last, before := pages[len(pages)-1], pages[len(pages)-2]
		want := int64(300 * 960)
		if int64(ns) > 0 {
			want = before.granule // 255 packets filled the page before
		}
		if last.granule != want {
			t.Errorf("padding %d ns: last granule %d, want %d", int64(ns), last.granule, want)
		}
	}
}

// A page closes at 255 segments, however small the packets.
func TestPageClosesAt255Segments(t *testing.T) {
	one := packet(0xF8, 0)
	var many [][]byte
	for range 300 {
		many = append(many, one)
	}
	var out bytes.Buffer
	if err := remux(bytes.NewReader(testWebM(many, 0, false)), &out, Tags{}); err != nil {
		t.Fatal(err)
	}
	pages := readPages(t, out.Bytes())
	if len(pages) != 4 || len(pages[2].segs) != 255 || len(pages[3].segs) != 45 {
		t.Fatalf("%d pages", len(pages))
	}
	if pages[2].granule != 255*960 || pages[3].granule != 300*960 {
		t.Fatalf("granules %d, %d", pages[2].granule, pages[3].granule)
	}
	if packets, _ := packetsOf(pages); len(packets) != 302 {
		t.Fatalf("%d packets", len(packets))
	}
}

func TestCutAfterTheLastCluster(t *testing.T) {
	cues := el(0x1C53BB6B, el(0xBB, uintEl(0xB3, 0)))
	file := testWebM(testPackets, 0, false)
	cut := file[:len(file)-len(cues)]
	if err := remux(bytes.NewReader(cut), &bytes.Buffer{}, Tags{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("cut where the segment says more follows: %v", err)
	}
}

func TestHostileFiles(t *testing.T) {
	var entries [][]byte
	for range 40 {
		entries = append(entries, el(idTrackEntry))
	}
	crowded := append(el(idEBML), el(idSegment, el(idTracks, entries...), el(idCluster))...)
	if err := remux(bytes.NewReader(crowded), &bytes.Buffer{}, Tags{}); !errors.Is(err, errFormat) {
		t.Errorf("40 tracks: %v", err)
	}
	big := el(idBlockGroup, el(idBlock, make([]byte, maxBlock+1)))
	if err := remux(bytes.NewReader(opusWebM(big)), &bytes.Buffer{}, Tags{}); !errors.Is(err, errTooBig) {
		t.Errorf("oversized block: %v", err)
	}
	// Cut before any cluster: the file is short, not Opus-less.
	head := testWebM(testPackets, 0, false)
	head = head[:bytes.Index(head, ebmlID(idCluster))]
	if err := remux(bytes.NewReader(head), &bytes.Buffer{}, Tags{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("cut before the first cluster: %v", err)
	}
}
