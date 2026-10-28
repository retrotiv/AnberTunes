package audio

import "math"

// WithGain scales src by db decibels, held down so that peak (the loudest
// sample, 1 = full scale, 0 if unknown) doesn't clip. Without a known peak the
// gain is never a boost.
func WithGain(src Source, db, peak float64) Source {
	k := math.Pow(10, db/20)
	if peak <= 0 {
		peak = 1
	}
	if peak*k > 1 {
		k = 1 / peak
	}
	return &gained{Source: src, k: float32(k)}
}

type gained struct {
	Source
	k float32
}

func (g *gained) Read(buf []float32) (int, error) {
	n, err := g.Source.Read(buf)
	for i := range buf[:2*n] {
		buf[i] *= g.k
	}
	return n, err
}
