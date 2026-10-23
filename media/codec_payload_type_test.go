// SPDX-License-Identifier: MPL-2.0
// SPDX-FileCopyrightText: Copyright (c) 2024, Emir Aganovic

package media

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRTPWithDifferentDynamicPayloadTypes negotiates Opus between an offerer
// using PT 111 and an answerer whose local Opus is on PT 96, then exchanges
// RTP over UDP loopback in both directions with the negotiated codec.
func TestRTPWithDifferentDynamicPayloadTypes(t *testing.T) {
	opus111 := CodecAudioOpus
	opus111.PayloadType = 111

	newSession := func(codec Codec) *MediaSession {
		m := &MediaSession{
			Codecs: []Codec{codec},
			Laddr:  net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0},
			Mode:   "sendrecv",
		}
		require.NoError(t, m.Init())
		t.Cleanup(func() { m.Close() })
		return m
	}
	offerer := newSession(opus111)
	answerer := newSession(CodecAudioOpus)

	require.NoError(t, answerer.RemoteSDP(offerer.LocalSDP()))
	require.NoError(t, offerer.RemoteSDP(answerer.LocalSDP()))
	require.Equal(t, uint8(111), CodecAudioFromSession(answerer).PayloadType)
	require.Equal(t, uint8(111), CodecAudioFromSession(offerer).PayloadType)

	send := func(t *testing.T, from, to *MediaSession) {
		t.Helper()
		writer := NewRTPPacketWriterSession(NewRTPSession(from))
		reader := NewRTPPacketReaderSession(NewRTPSession(to))
		require.NoError(t, to.rtpConn.SetReadDeadline(time.Now().Add(time.Second)))
		_, err := writer.Write(make([]byte, 40))
		require.NoError(t, err, "writing the negotiated payload type")
		_, err = reader.Read(make([]byte, 1500))
		require.NoError(t, err, "reading the negotiated payload type")
		assert.Equal(t, uint8(111), reader.PacketHeader.PayloadType)
	}
	t.Run("offerer to answerer", func(t *testing.T) { send(t, offerer, answerer) })
	t.Run("answerer to offerer", func(t *testing.T) { send(t, answerer, offerer) })
}

// TestCodecByPayloadTypeAcceptsNegotiatedAndLocalPayloadTypes covers an
// answerer that keeps its own payload type: the offerer sends with the
// negotiated PT and must still accept media on the PT it offered.
func TestCodecByPayloadTypeAcceptsNegotiatedAndLocalPayloadTypes(t *testing.T) {
	opus111 := CodecAudioOpus
	opus111.PayloadType = 111
	m := &MediaSession{Codecs: []Codec{CodecAudioOpus}, filterCodecs: []Codec{opus111}}
	for _, pt := range []uint8{96, 111} {
		codec, ok := m.codecByPayloadType(pt)
		assert.True(t, ok, "payload type %d", pt)
		assert.Equal(t, "opus", codec.Name)
	}
	_, ok := m.codecByPayloadType(0)
	assert.False(t, ok, "a payload type that was never offered or negotiated must be rejected")
}

// TestDTMFUsesNegotiatedTelephoneEventPayloadType sends RFC 4733 DTMF from an
// answerer whose local telephone-event is on PT 101 to an offerer using PT
// 100: both sides must use the negotiated PT 100.
func TestDTMFUsesNegotiatedTelephoneEventPayloadType(t *testing.T) {
	event100 := CodecTelephoneEvent8000
	event100.PayloadType = 100
	newSession := func(codecs ...Codec) *MediaSession {
		m := &MediaSession{Codecs: codecs, Laddr: net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, Mode: "sendrecv"}
		require.NoError(t, m.Init())
		t.Cleanup(func() { m.Close() })
		return m
	}
	offerer := newSession(CodecAudioUlaw, event100)
	answerer := newSession(CodecAudioUlaw, CodecTelephoneEvent8000)
	require.NoError(t, answerer.RemoteSDP(offerer.LocalSDP()))
	require.NoError(t, offerer.RemoteSDP(answerer.LocalSDP()))
	require.Equal(t, uint8(100), CodecTelephoneEventFromSession(answerer).PayloadType)
	require.Equal(t, uint8(100), CodecTelephoneEventFromSession(offerer).PayloadType)

	packetWriter := NewRTPPacketWriterSession(NewRTPSession(answerer))
	dtmfWriter := NewRTPDTMFWriter(CodecTelephoneEventFromSession(answerer), packetWriter, packetWriter)
	packetReader := NewRTPPacketReaderSession(NewRTPSession(offerer))
	dtmfReader := NewRTPDTMFReader(CodecTelephoneEventFromSession(offerer), packetReader, packetReader)

	require.NoError(t, offerer.rtpConn.SetReadDeadline(time.Now().Add(2*time.Second)))
	writeDone := make(chan error, 1)
	go func() { writeDone <- dtmfWriter.WriteDTMF('5') }()
	buf := make([]byte, 1500)
	for {
		_, err := dtmfReader.Read(buf)
		require.NoError(t, err, "no DTMF received on the negotiated payload type")
		assert.Equal(t, uint8(100), packetReader.PacketHeader.PayloadType)
		if dtmf, ok := dtmfReader.ReadDTMF(); ok {
			assert.Equal(t, '5', dtmf)
			break
		}
	}
	require.NoError(t, <-writeDone)
}

func TestCodecsFromSDPReadSkipsPayloadTypesOutsideRTPRange(t *testing.T) {
	codecs := make([]Codec, 3)
	n, _ := CodecsFromSDPRead([]string{"127", "128", "200"}, []string{
		"rtpmap:127 opus/48000/2", "rtpmap:128 opus/48000/2", "rtpmap:200 opus/48000/2",
	}, codecs)
	require.Equal(t, 1, n)
	assert.Equal(t, uint8(127), codecs[0].PayloadType)
}
