package zello

import "testing"

func TestCodecHeaderRoundTrip(t *testing.T) {
	encoded := MakeCodecHeader(16000, 1, 20)
	if encoded != "gD4BFA==" {
		t.Fatalf("encoded = %q", encoded)
	}
	header, err := ParseCodecHeader(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if header.SampleRate != 16000 || header.FramesPerPacket != 1 || header.FrameSizeMS != 20 {
		t.Fatalf("header = %+v", header)
	}
}

func TestAudioPacketRoundTrip(t *testing.T) {
	packet := EncodeAudioPacket(42, []byte{1, 2, 3})
	streamID, packetID, payload, err := DecodeAudioPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if streamID != 42 || packetID != 0 || string(payload) != string([]byte{1, 2, 3}) {
		t.Fatalf("decoded stream=%d packet=%d payload=%v", streamID, packetID, payload)
	}
}
