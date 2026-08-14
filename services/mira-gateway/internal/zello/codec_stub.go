//go:build !opus

package zello

import "errors"

type OpusFactory struct{}

func NewOpusFactory() *OpusFactory { return &OpusFactory{} }

func (f *OpusFactory) NewDecoder(int) (Decoder, error) {
	return nil, errors.New("opus support was not compiled; build with -tags opus")
}

func (f *OpusFactory) NewEncoder(int) (Encoder, error) {
	return nil, errors.New("opus support was not compiled; build with -tags opus")
}

func (f *OpusFactory) Ready() bool { return false }
