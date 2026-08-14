package audio

import "errors"

type Resampler interface {
	Resample(input []int16, fromRate, toRate int) ([]int16, error)
}

type LinearResampler struct{}

func (LinearResampler) Resample(input []int16, fromRate, toRate int) ([]int16, error) {
	if fromRate <= 0 || toRate <= 0 {
		return nil, errors.New("sample rates must be positive")
	}
	if fromRate == toRate || len(input) == 0 {
		out := make([]int16, len(input))
		copy(out, input)
		return out, nil
	}
	outLen := len(input) * toRate / fromRate
	if outLen == 0 {
		return nil, nil
	}
	out := make([]int16, outLen)
	ratio := float64(fromRate) / float64(toRate)
	for i := range out {
		pos := float64(i) * ratio
		j := int(pos)
		if j >= len(input)-1 {
			out[i] = input[len(input)-1]
			continue
		}
		frac := pos - float64(j)
		out[i] = int16(float64(input[j])*(1-frac) + float64(input[j+1])*frac)
	}
	return out, nil
}
