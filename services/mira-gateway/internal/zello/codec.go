package zello

type Decoder interface {
	Decode(packet []byte) ([]int16, error)
	Close() error
}

type Encoder interface {
	Encode(pcm []int16) ([]byte, error)
	Close() error
}

type CodecFactory interface {
	NewDecoder(sampleRate int) (Decoder, error)
	NewEncoder(sampleRate int) (Encoder, error)
	Ready() bool
}
