package zello

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	BinaryTypeAudio byte = 0x01
)

type LogonRequest struct {
	Command      string            `json:"command"`
	Seq          uint32            `json:"seq"`
	AuthToken    string            `json:"auth_token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	Username     string            `json:"username,omitempty"`
	Password     string            `json:"password,omitempty"`
	Channels     []string          `json:"channels"`
	Version      string            `json:"version,omitempty"`
	PlatformType string            `json:"platform_type,omitempty"`
	PlatformName string            `json:"platform_name,omitempty"`
	Features     map[string]bool   `json:"features,omitempty"`
	Extra        map[string]string `json:"-"`
}

type StartStreamRequest struct {
	Command        string `json:"command"`
	Seq            uint32 `json:"seq"`
	Channel        string `json:"channel"`
	Type           string `json:"type"`
	Codec          string `json:"codec"`
	CodecHeader    string `json:"codec_header"`
	PacketDuration int    `json:"packet_duration"`
	For            string `json:"for,omitempty"`
}

type StopStreamRequest struct {
	Command  string `json:"command"`
	Seq      uint32 `json:"seq"`
	StreamID uint32 `json:"stream_id"`
	Channel  string `json:"channel"`
}

type SendTextMessageRequest struct {
	Command string `json:"command"`
	Channel string `json:"channel"`
	Text    string `json:"text"`
	For     string `json:"for,omitempty"`
}

type Response struct {
	Seq          uint32 `json:"seq"`
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	StreamID     uint32 `json:"stream_id"`
	RefreshToken string `json:"refresh_token"`
}

type Event struct {
	Command             string          `json:"command"`
	Channel             string          `json:"channel"`
	Status              string          `json:"status"`
	UsersOnline         int             `json:"users_online"`
	Type                string          `json:"type"`
	Codec               string          `json:"codec"`
	CodecHeader         string          `json:"codec_header"`
	PacketDuration      int             `json:"packet_duration"`
	StreamID            uint32          `json:"stream_id"`
	From                string          `json:"from"`
	For                 json.RawMessage `json:"for"`
	MessageID           uint32          `json:"message_id"`
	Text                string          `json:"text"`
	Error               string          `json:"error"`
	TranslationsEnabled bool            `json:"translations_enabled"`
	Language            string          `json:"language"`
}

type CodecHeader struct {
	SampleRate      int
	FramesPerPacket int
	FrameSizeMS     int
}

func MakeCodecHeader(sampleRate, framesPerPacket, frameSizeMS int) string {
	buf := []byte{0, 0, byte(framesPerPacket), byte(frameSizeMS)}
	binary.LittleEndian.PutUint16(buf[:2], uint16(sampleRate))
	return base64.StdEncoding.EncodeToString(buf)
}

func ParseCodecHeader(encoded string) (CodecHeader, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return CodecHeader{}, err
	}
	if len(raw) != 4 {
		return CodecHeader{}, fmt.Errorf("zello codec_header length=%d, want 4", len(raw))
	}
	return CodecHeader{
		SampleRate:      int(binary.LittleEndian.Uint16(raw[:2])),
		FramesPerPacket: int(raw[2]),
		FrameSizeMS:     int(raw[3]),
	}, nil
}

func EncodeAudioPacket(streamID uint32, opus []byte) []byte {
	out := make([]byte, 9+len(opus))
	out[0] = BinaryTypeAudio
	binary.BigEndian.PutUint32(out[1:5], streamID)
	binary.BigEndian.PutUint32(out[5:9], 0)
	copy(out[9:], opus)
	return out
}

func DecodeAudioPacket(packet []byte) (streamID uint32, packetID uint32, payload []byte, err error) {
	if len(packet) < 9 {
		return 0, 0, nil, errors.New("zello audio packet too short")
	}
	if packet[0] != BinaryTypeAudio {
		return 0, 0, nil, fmt.Errorf("unsupported zello binary packet type 0x%x", packet[0])
	}
	return binary.BigEndian.Uint32(packet[1:5]), binary.BigEndian.Uint32(packet[5:9]), packet[9:], nil
}

func IsResponse(data []byte) (Response, bool) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, false
	}
	return resp, resp.Seq != 0 && resp.Commandless()
}

func (r Response) Commandless() bool {
	return r.Success || r.Error != "" || r.StreamID != 0 || r.RefreshToken != ""
}
