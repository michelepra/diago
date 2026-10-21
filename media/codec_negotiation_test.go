// SPDX-License-Identifier: MPL-2.0
// SPDX-FileCopyrightText: Copyright (c) 2024, Emir Aganovic

package media

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/diago/media/sdp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var codecAMRWB = Codec{Name: "AMR-WB", PayloadType: 112, SampleRate: 16000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Fmtp: "octet-align=1"}

func answerer(t *testing.T, codecs ...Codec) *MediaSession {
	t.Helper()
	m := &MediaSession{
		Codecs: codecs,
		Laddr:  net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0},
		Mode:   "sendrecv",
	}
	require.NoError(t, m.Init())
	t.Cleanup(func() { m.Close() })
	return m
}

func offer(formats string, attrs ...string) []byte {
	lines := []string{
		"v=0",
		"o=- 1 1 IN IP4 192.168.178.54",
		"s=-",
		"c=IN IP4 192.168.178.54",
		"t=0 0",
		"m=audio 34391 RTP/AVP " + formats,
	}
	for _, a := range attrs {
		lines = append(lines, "a="+a)
	}
	lines = append(lines, "a=sendrecv")
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

func TestDynamicCodecUsesOfferedPayloadTypeAndFmtp(t *testing.T) {
	m := answerer(t, CodecAudioUlaw, codecAMRWB)
	require.NoError(t, m.RemoteSDP(offer("98 0", "rtpmap:98 AMR-WB/16000", "fmtp:98 octet-align=1;mode-set=0,1,2", "rtpmap:0 PCMU/8000")))

	require.Len(t, m.filterCodecs, 2)
	negotiated := CodecAudioFromSession(m)
	assert.Equal(t, "AMR-WB", negotiated.Name)
	assert.Equal(t, uint8(98), negotiated.PayloadType, "the answerer must use the offerer's dynamic payload type")
	assert.Equal(t, "octet-align=1;mode-set=0,1,2", negotiated.Fmtp)

	answer := sdp.SessionDescription{}
	require.NoError(t, sdp.Unmarshal(m.LocalSDP(), &answer))
	assert.True(t, strings.HasSuffix(answer.Value("m"), " RTP/AVP 98 0"), "answer m-line %q", answer.Value("m"))
	attrs := strings.Join(answer.Values("a"), "\n")
	assert.Contains(t, attrs, "rtpmap:98 AMR-WB/16000/1")
	assert.Contains(t, attrs, "fmtp:98 octet-align=1;mode-set=0,1,2")
}

func TestOfferWritesLocalFmtp(t *testing.T) {
	m := answerer(t, codecAMRWB)
	offer := sdp.SessionDescription{}
	require.NoError(t, sdp.Unmarshal(m.LocalSDP(), &offer))
	attrs := strings.Join(offer.Values("a"), "\n")
	assert.Contains(t, attrs, "rtpmap:112 AMR-WB/16000/1")
	assert.Contains(t, attrs, "fmtp:112 octet-align=1")
}

func TestDynamicOpusOnAnotherPayloadTypeIsNegotiated(t *testing.T) {
	m := answerer(t, CodecAudioOpus)
	require.NoError(t, m.RemoteSDP(offer("111", "rtpmap:111 opus/48000/2", "fmtp:111 minptime=10;useinbandfec=1")))

	negotiated := CodecAudioFromSession(m)
	assert.Equal(t, uint8(111), negotiated.PayloadType)
	answer := sdp.SessionDescription{}
	require.NoError(t, sdp.Unmarshal(m.LocalSDP(), &answer))
	attrs := strings.Join(answer.Values("a"), "\n")
	assert.Contains(t, attrs, "rtpmap:111 opus/48000/2")
	assert.Contains(t, attrs, "fmtp:111 useinbandfec=0")
}

func TestDynamicCodecDoesNotMatchDifferentEncoding(t *testing.T) {
	m := answerer(t, codecAMRWB)
	err := m.RemoteSDP(offer("112", "rtpmap:112 AMR/8000"))
	require.Error(t, err, "AMR narrowband must not match AMR-WB on the same payload type")
}

func TestStaticPayloadTypeWithoutRtpmap(t *testing.T) {
	g722 := Codec{Name: "G722", PayloadType: 9, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1}
	m := answerer(t, g722)
	require.NoError(t, m.RemoteSDP(offer("9")))
	assert.Equal(t, g722, CodecAudioFromSession(m))

	g729 := Codec{Name: "G729", PayloadType: 18, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1}
	m = answerer(t, g729)
	require.NoError(t, m.RemoteSDP(offer("18", "fmtp:18 annexb=no")))
	g729.Fmtp = "annexb=no"
	assert.Equal(t, g729, CodecAudioFromSession(m))
}
