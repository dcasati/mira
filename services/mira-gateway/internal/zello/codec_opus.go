//go:build opus

package zello

import opus "gopkg.in/hraban/opus.v2"

const opusChannels = 1

type OpusFactory struct{}

func NewOpusFactory() *OpusFactory { return &OpusFactory{} }

func (f *OpusFactory) NewDecoder(sampleRate int) (Decoder, error) {
	d, err := opus.NewDecoder(sampleRate, opusChannels)
	if err != nil {
		return nil, err
	}
	return &opusDecoder{d: d, sampleRate: sampleRate}, nil
}

func (f *OpusFactory) NewEncoder(sampleRate int) (Encoder, error) {
	e, err := opus.NewEncoder(sampleRate, opusChannels, opus.AppVoIP)
	if err != nil {
		return nil, err
	}
	return &opusEncoder{e: e}, nil
}

func (f *OpusFactory) Ready() bool { return true }

type opusDecoder struct {
	d          *opus.Decoder
	sampleRate int
}

func (d *opusDecoder) Decode(packet []byte) ([]int16, error) {
	max := d.sampleRate * 120 / 1000
	pcm := make([]int16, max)
	n, err := d.d.Decode(packet, pcm)
	if err != nil {
		return nil, err
	}
	return pcm[:n], nil
}

func (d *opusDecoder) Close() error { return nil }

type opusEncoder struct {
	e *opus.Encoder
}

func (e *opusEncoder) Encode(pcm []int16) ([]byte, error) {
	out := make([]byte, 4000)
	n, err := e.e.Encode(pcm, out)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func (e *opusEncoder) Close() error { return nil }
