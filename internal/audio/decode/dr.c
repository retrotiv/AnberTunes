// Builds the single-file decoders vendored in this directory, and the MP3
// opening and seeking that need dr_mp3's internals.
#define DR_MP3_FLOAT_OUTPUT // what Read wants, so samples aren't cut to 16 bits on the way
#define DR_FLAC_NO_OGG      // FLAC in Ogg isn't among the formats opened
#define DR_FLAC_NO_WCHAR
#define DR_MP3_IMPLEMENTATION
#include "dr_mp3.h"
#define DR_FLAC_IMPLEMENTATION
#include "dr_flac.h"

/*
MP3 seeking.

minimp3 decodes a frame only once it holds the frame's bit reservoir, data
that can start up to 511 bytes back in the frames before, and its output also
depends on the two frames before through the IMDCT overlap and the synthesis
filter. So a seek has to decode from a little before its target. dr_mp3's
seek table restarts there but counts only the frames minimp3 outputs, so it
lands one or two frames out wherever minimp3 dropped some for want of
reservoir, and its currentPCMFrame then disagrees with the audio, which
spoils the trim at the end. Reading from the start is exact, but takes about a
second per three minutes of audio on the Brick.

So the frame headers are walked once, as far as seeks need, recording where
every nth frame starts. A seek restarts the decoder at a recorded frame at
least lookback frames before its target, feeds it one frame at a time,
counting frames by their headers so the ones minimp3 drops still count, and
leaves dr_mp3 where a straight decode would be.

What this relies on in dr_mp3 (vendored at dfe837763100):
- currentPCMFrame counts frames from the start of the stream, encoder delay
  included: drmp3_read_pcm_frames_raw skips output while it is below
  delayInPCMFrames and stops at totalPCMFrameCount - paddingInPCMFrames.
- The frame being read is held in pcmFrames (in drmp3d_sample_t), with
  pcmFramesConsumedInMP3Frame and pcmFramesRemainingInMP3Frame counting
  through it and mp3FrameChannels its layout.
- Unread input waits in pData from dataConsumed for dataSize bytes, topped up
  as drmp3_decode_next_frame_ex__callbacks does, and streamCursor is the
  stream position after it. drmp3_reset empties it and restarts minimp3.
- drmp3dec_decode_frame finds the first frame after a restart with
  drmp3d_find_frame. A later frame is decoded if its header matches the last
  one's and the next header matches too, or it ends the data; otherwise
  minimp3 restarts. A frame whose reservoir isn't all there gives no output
  but is consumed, and a NULL pcm only moves the reservoir on.
  drmp3_L3_save_reservoir keeps at most 511 bytes after the last bit read, so
  a straight decode's reservoir follows from the side info alone.
*/

enum {
	bb_lookback = 10,   // frames before a target: enough reservoir at almost any bitrate
	bb_near = 5,        // seconds: forward seeks nearer than this just read on
	bb_chunk = 1 << 16, // bytes read at a time when walking the headers
};

typedef struct {
	drmp3_uint64* at;       // byte offset of every nth frame
	drmp3_uint32 n, cap;    // entries in at
	drmp3_uint32 every;     // frames between entries, about a second's worth
	drmp3_uint32 spf;       // PCM frames per MP3 frame
	drmp3_uint64 frames;    // frames walked, every one of them decoded by a straight run
	drmp3_uint64 lead;      // frames at the start minimp3 drops for want of reservoir
	drmp3_uint64 next;      // byte offset of the frame after the last walked
	int reserv;             // reservoir bytes minimp3 holds after the last walked frame
	int walked;             // 0 not started, 1 part-way, 2 as far as it goes
	drmp3_uint8 hdr[4];     // the first frame's header; the rest must match it
} bb_mp3_index;

// The drmp3 comes first, so the pointer handed out converts back.
typedef struct {
	drmp3 m;
	bb_mp3_index ix;
} bb_mp3;

drmp3* bb_mp3_open(const char* path)
{
	bb_mp3* f = (bb_mp3*)calloc(1, sizeof(bb_mp3));
	if (f == NULL) return NULL;
	if (!drmp3_init_file(&f->m, path, NULL)) {
		free(f);
		return NULL;
	}
	// Past a Xing frame dr_mp3 sets streamCursor back to the end of that
	// frame although it has already read further, so reads are no longer
	// stopped at an ID3v1 or APE tag and the tag costs the last frame.
	long at = ftell((FILE*)f->m.pUserData);
	if (at >= 0) f->m.streamCursor = (drmp3_uint64)at;
	return &f->m;
}

void bb_mp3_close(drmp3* m)
{
	bb_mp3* f = (bb_mp3*)m;
	free(f->ix.at);
	drmp3_uninit(m);
	free(f);
}

// Whether the frame holding raw PCM frame r has been walked.
static int bb_walked(const bb_mp3_index* ix, drmp3_uint64 r)
{
	return ix->spf > 0 && ix->frames > ix->lead + r / ix->spf;
}

// Walks frames that a straight decode plays one after another, until the one
// holding raw PCM frame r, recording every nth. The walk stops for good at a
// frame minimp3 would drop or restart on. The stream is left where it was.
static int bb_walk(drmp3* m, bb_mp3_index* ix, drmp3_uint64 r)
{
	drmp3_uint64 back = m->streamCursor;
	size_t len = 0, pos = 0;
	int end = 0;
	drmp3_uint8* buf;
	if (bb_walked(ix, r) || ix->walked == 2) return bb_walked(ix, r);
	buf = (drmp3_uint8*)malloc(bb_chunk);
	if (buf == NULL) return 0;
	if (!drmp3__on_seek_64(m, ix->walked ? ix->next : m->streamStartOffset, DRMP3_SEEK_SET)) {
		ix->walked = 2;
	}
	while (ix->walked != 2 && !bb_walked(ix, r)) {
		const drmp3_uint8* h;
		int size;
		if (!end && len - pos < bb_chunk / 2) {
			size_t want, got;
			memmove(buf, buf + pos, len - pos);
			len -= pos;
			pos = 0;
			want = bb_chunk - len;
			got = drmp3__on_read_clamped(m, buf + len, want);
			len += got;
			end = got < want;
		}
		h = buf + pos;
		if (!ix->walked) {
			int free_bytes = 0, bytes;
			int i = drmp3d_find_frame(buf, (int)len, &free_bytes, &bytes);
			if (bytes == 0 || free_bytes != 0) break; // nothing, or free format
			memcpy(ix->hdr, buf + i, 4);
			ix->spf = drmp3_hdr_frame_samples(ix->hdr);
			ix->every = DRMP3_MAX(drmp3_hdr_sample_rate_hz(ix->hdr) / ix->spf, 1);
			ix->next = m->streamStartOffset + i;
			ix->walked = 1;
			pos = i;
			continue;
		}
		if (len - pos < DRMP3_HDR_SIZE || !drmp3_hdr_compare(ix->hdr, h)) break;
		size = drmp3_hdr_frame_bytes(h, 0) + drmp3_hdr_padding(h);
		if (!(end && pos + size == len) && (pos + size + DRMP3_HDR_SIZE > len || !drmp3_hdr_compare(h, h + size))) break;
		if (4 - DRMP3_HDR_GET_LAYER(h) == 3) {
			drmp3_bs bs;
			drmp3_L3_gr_info gr[4];
			int begin, data, used = 0, i;
			drmp3_bs_init(&bs, h + DRMP3_HDR_SIZE, size - DRMP3_HDR_SIZE);
			if (DRMP3_HDR_IS_CRC(h)) drmp3_bs_get_bits(&bs, 16);
			begin = drmp3_L3_read_side_info(&bs, gr, h);
			if (begin < 0 || bs.pos > bs.limit) break;
			data = (bs.limit - bs.pos) / 8;
			if (ix->reserv >= begin) {
				for (i = 0; i < (DRMP3_HDR_TEST_MPEG1(h) ? 2 : 1) * (DRMP3_HDR_IS_MONO(h) ? 1 : 2); i++) {
					used += gr[i].part_23_length;
				}
				ix->reserv = DRMP3_MIN(begin + data - (used + 7) / 8, DRMP3_MAX_BITRESERVOIR_BYTES);
			} else if (ix->frames > ix->lead) {
				break; // dropped part-way through: frames after it would be counted wrong
			} else {
				ix->reserv = DRMP3_MIN(ix->reserv + data, DRMP3_MAX_BITRESERVOIR_BYTES);
				ix->lead++;
			}
		}
		if (ix->frames % ix->every == 0) {
			if (ix->n == ix->cap) {
				drmp3_uint32 cap = ix->cap ? 2 * ix->cap : 256;
				drmp3_uint64* at = (drmp3_uint64*)realloc(ix->at, cap * sizeof(*at));
				if (at == NULL) break;
				ix->at = at;
				ix->cap = cap;
			}
			ix->at[ix->n++] = ix->next;
		}
		ix->frames++;
		ix->next += size;
		pos += size;
	}
	if (!bb_walked(ix, r)) ix->walked = 2;
	free(buf);
	drmp3__on_seek_64(m, back, DRMP3_SEEK_SET);
	return bb_walked(ix, r);
}

// Tops up dr_mp3's input as drmp3_decode_next_frame_ex__callbacks does, so
// minimp3 sees frames with the same data after them as in a straight decode.
static int bb_fill(drmp3* m)
{
	if (m->dataSize >= DRMP3_MIN_DATA_CHUNK_SIZE) return 1;
	if (m->pData != NULL) DRMP3_MOVE_MEMORY(m->pData, m->pData + m->dataConsumed, m->dataSize);
	m->dataConsumed = 0;
	if (m->dataCapacity < DRMP3_DATA_CHUNK_SIZE) {
		drmp3_uint8* p = (drmp3_uint8*)drmp3__realloc_from_callbacks(m->pData, DRMP3_DATA_CHUNK_SIZE, m->dataCapacity, &m->allocationCallbacks);
		if (p == NULL) return 0;
		m->pData = p;
		m->dataCapacity = DRMP3_DATA_CHUNK_SIZE;
	}
	m->dataSize += drmp3__on_read_clamped(m, m->pData + m->dataSize, m->dataCapacity - m->dataSize);
	return 1;
}

// Restarts the decoder at entry e of the index and decodes up to frame t,
// then puts dr_mp3 o PCM frames into it. Frames before t-2 only fill the
// reservoir; t-2 and t-1 rebuild the overlap and filter state. It returns 0
// if the reservoir wasn't full by then, so an earlier start is needed, and -1
// on an error.
static int bb_decode_to(drmp3* m, const bb_mp3_index* ix, drmp3_uint32 e, drmp3_uint64 t, drmp3_uint32 o)
{
	drmp3dec_frame_info info;
	drmp3_uint64 j;
	if (!drmp3__on_seek_64(m, ix->at[e], DRMP3_SEEK_SET)) return -1;
	drmp3_reset(m);
	for (j = (drmp3_uint64)e * ix->every; j <= t; j++) {
		const drmp3_uint8* h;
		int size, n, out = j + 2 >= t;
		if (!bb_fill(m)) return -1;
		h = m->pData + m->dataConsumed;
		if (m->dataSize < DRMP3_HDR_SIZE || !drmp3_hdr_compare(ix->hdr, h)) return -1;
		size = drmp3_hdr_frame_bytes(h, 0) + drmp3_hdr_padding(h);
		n = drmp3dec_decode_frame(&m->decoder, h, (int)m->dataSize, out ? (drmp3d_sample_t*)m->pcmFrames : NULL, &info);
		if (info.frame_bytes != size) return -1;
		m->dataConsumed += size;
		m->dataSize -= size;
		if (out && n == 0) return 0;
	}
	m->mp3FrameChannels = info.channels;
	m->mp3FrameSampleRate = info.sample_rate;
	m->pcmFramesConsumedInMP3Frame = o;
	m->pcmFramesRemainingInMP3Frame = ix->spf - o;
	m->currentPCMFrame = (t - ix->lead) * ix->spf + o;
	return 1;
}

// Seeks to raw PCM frame r (the encoder delay counted) through the index. It
// returns 1 when done, -1 when the index can't reach r and the decoder is
// untouched, and 0 when the decoder was left somewhere else.
static int bb_seek_indexed(bb_mp3* f, drmp3_uint64 r)
{
	drmp3_uint64 t;
	drmp3_uint32 e, tries;
	if (!bb_walk(&f->m, &f->ix, r)) return -1;
	t = f->ix.lead + r / f->ix.spf;
	if (t < bb_lookback) return -1;
	e = (drmp3_uint32)((t - bb_lookback) / f->ix.every);
	// One entry further back covers the reservoir even at the lowest bitrates.
	for (tries = 0; tries < 2; tries++, e--) {
		int ok = bb_decode_to(&f->m, &f->ix, e, t, (drmp3_uint32)(r % f->ix.spf));
		if (ok != 0 || e == 0) return ok > 0;
	}
	return 0;
}

// Seeks to output frame frame, with the stream at output frame cur. Short
// seeks forward read on; others go through the index, or else read from the
// start. dr_mp3's own seek counts the encoder delay in the position it is
// given, and its seek table lands in the wrong place (see above).
int bb_mp3_seek(drmp3* m, drmp3_uint64 frame, drmp3_uint64 cur)
{
	drmp3_uint64 nearby = (drmp3_uint64)m->sampleRate * bb_near;
	int ok = -1;
	if ((frame < cur || frame - cur >= nearby) && frame >= nearby) {
		ok = bb_seek_indexed((bb_mp3*)m, frame + m->delayInPCMFrames);
		if (ok > 0) return 1;
	}
	if (frame < cur || ok == 0) {
		if (!drmp3_seek_to_pcm_frame(m, 0)) return 0;
		cur = 0;
	}
	return drmp3_read_pcm_frames_f32(m, frame - cur, NULL) == frame - cur;
}
