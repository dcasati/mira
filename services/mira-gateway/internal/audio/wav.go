package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

func ReadWAVPCM16Mono(path string) (PCM, error) {
	f, err := os.Open(path)
	if err != nil {
		return PCM{}, err
	}
	defer f.Close()
	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return PCM{}, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return PCM{}, errors.New("not a RIFF/WAVE file")
	}
	var sampleRate int
	var bitsPerSample uint16
	var channels uint16
	var data []byte
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			return PCM{}, err
		}
		chunkID := string(chunkHeader[:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:])
		chunk := make([]byte, chunkSize)
		if _, err := io.ReadFull(f, chunk); err != nil {
			return PCM{}, err
		}
		if chunkSize%2 == 1 {
			if _, err := f.Seek(1, io.SeekCurrent); err != nil {
				return PCM{}, err
			}
		}
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return PCM{}, errors.New("invalid wav fmt chunk")
			}
			format := binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			if format != 1 {
				return PCM{}, fmt.Errorf("unsupported wav format %d", format)
			}
		case "data":
			data = chunk
		}
		if sampleRate != 0 && data != nil {
			break
		}
	}
	if channels != 1 || bitsPerSample != 16 {
		return PCM{}, fmt.Errorf("expected mono PCM16 wav, got channels=%d bits=%d", channels, bitsPerSample)
	}
	samples, err := BytesToInt16(data)
	if err != nil {
		return PCM{}, err
	}
	return PCM{Samples: samples, SampleRate: sampleRate}, nil
}
