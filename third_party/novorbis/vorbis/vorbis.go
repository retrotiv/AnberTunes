// Package vorbis stands in for github.com/xlab/vorbis-go/vorbis, the
// libvorbis bindings go-librespot's player decodes with. Boombrick's Spotify
// helper decodes its streams itself, but go-librespot's session package links
// the player all the same; this keeps the bindings, which come with no
// licence, and libvorbis out of the helper. go.mod replaces the module with
// it.
//
// It has what go-librespot's vorbis package uses, and finds nothing: a page
// is never synced, so a decoder made with it fails on its first header, as it
// would on data that isn't Ogg Vorbis.
package vorbis

// OggInt64 is libogg's ogg_int64_t.
type OggInt64 int64

type (
	OggSyncState   struct{ Returned int32 }
	OggStreamState struct{}
	OggPage        struct{}
	OggPacket      struct {
		Packet []byte
		Bytes  int
	}
	Info struct {
		Channels int32
		Rate     int
	}
	Comment  struct{}
	DspState struct{}
	Block    struct{}
)

func (x *OggSyncState) Free()                  {}
func (x *OggSyncState) Deref()                 {}
func (x *OggStreamState) Free()                {}
func (x *OggStreamState) Ref() *OggStreamState { return nil }
func (x *OggPage) Free()                       {}
func (x *OggPacket) Free()                     {}
func (x *OggPacket) Deref()                    {}
func (x *Info) Free()                          {}
func (x *Info) Deref()                         {}
func (x *Info) Ref() *Info                     { return nil }
func (x *Comment) Free()                       {}
func (x *Comment) Deref()                      {}
func (x *Comment) Ref() *Comment               { return nil }
func (x *DspState) Free()                      {}
func (x *DspState) Ref() *DspState             { return nil }
func (x *Block) Free()                         {}
func (x *Block) Ref() *Block                   { return nil }
func OggSyncInit(oy *OggSyncState) int32       { return 0 }
func OggSyncClear(oy *OggSyncState) int32      { return 0 }
func OggSyncBuffer(oy *OggSyncState, size int) []byte {
	return make([]byte, size)
}
func OggSyncWrote(oy *OggSyncState, bytes int) int32               { return 0 }
func OggSyncPageout(oy *OggSyncState, og *OggPage) int32           { return 0 }
func OggStreamInit(os *OggStreamState, serialno int32) int32       { return 0 }
func OggStreamClear(os *OggStreamState) int32                      { return 0 }
func OggStreamPagein(os *OggStreamState, og *OggPage) int32        { return -1 }
func OggStreamPacketout(os *OggStreamState, op *OggPacket) int32   { return 0 }
func OggPageSerialno(og *OggPage) int32                            { return 0 }
func OggPageGranulepos(og *OggPage) OggInt64                       { return -1 }
func OggPageEos(og *OggPage) int32                                 { return 1 }
func InfoInit(vi *Info)                                            {}
func InfoClear(vi *Info)                                           {}
func CommentInit(vc *Comment)                                      {}
func CommentClear(vc *Comment)                                     {}
func SynthesisHeaderin(vi *Info, vc *Comment, op *OggPacket) int32 { return -1 }
func SynthesisInit(v *DspState, vi *Info) int32                    { return -1 }
func SynthesisPcmout(v *DspState, pcm [][][]float32) int32         { return 0 }
func SynthesisRead(v *DspState, samples int32) int32               { return 0 }
func SynthesisBlockin(v *DspState, vb *Block) int32                { return -1 }
func Synthesis(vb *Block, op *OggPacket) int32                     { return -1 }
func BlockInit(v *DspState, vb *Block) int32                       { return -1 }
func BlockClear(vb *Block) int32                                   { return 0 }
func DspClear(v *DspState)                                         {}
func GranuleTime(v *DspState, granulepos OggInt64) float64         { return 0 }
