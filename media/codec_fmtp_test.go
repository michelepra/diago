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

// AMR-WB in both RFC 4867 payload formats, as a local configuration would
// offer it.
var (
	codecAMRWB      = Codec{Name: "AMR-WB", PayloadType: 118, SampleRate: 16000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Fmtp: "octet-align=0"}
	codecAMRWBOctet = Codec{Name: "AMR-WB", PayloadType: 119, SampleRate: 16000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Fmtp: "octet-align=1"}
)

func fmtpSession(codecs ...Codec) *MediaSession {
	return &MediaSession{
		Codecs: codecs,
		Laddr:  net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
		Mode:   sdp.ModeSendrecv,
	}
}

func fmtpSDP(formats string, attrs ...string) []byte {
	lines := []string{"v=0", "o=- 1 1 IN IP4 127.0.0.1", "s=-", "c=IN IP4 127.0.0.1", "t=0 0", "m=audio 5678 RTP/AVP " + formats}
	for _, a := range attrs {
		lines = append(lines, "a="+a)
	}
	lines = append(lines, "a=sendrecv")
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

func TestOfferWritesLocalFmtp(t *testing.T) {
	offer := string(fmtpSession(codecAMRWB, codecAMRWBOctet).LocalSDP(false))
	assert.Contains(t, offer, "a=rtpmap:118 AMR-WB/16000\r\na=fmtp:118 octet-align=0\r\n")
	assert.Contains(t, offer, "a=rtpmap:119 AMR-WB/16000\r\na=fmtp:119 octet-align=1\r\n")
}

func TestAnswerEchoesOfferedFmtp(t *testing.T) {
	previousOrder := SDPCodecPreferLocalOrder
	t.Cleanup(func() { SDPCodecPreferLocalOrder = previousOrder })
	for _, localOrder := range []int{0, 1} {
		SDPCodecPreferLocalOrder = localOrder
		sess := fmtpSession(CodecAudioUlaw, codecAMRWB, codecAMRWBOctet)
		require.NoError(t, sess.RemoteSDP(fmtpSDP("98", "rtpmap:98 AMR-WB/16000", "fmtp:98 octet-align=1; mode-set=0,1,2"), false))

		require.Len(t, sess.CommonCodecs(), 1, "local order %d: one remote AMR-WB must give one negotiated codec", localOrder)
		negotiated := CodecAudioFromSession(sess)
		assert.Equal(t, uint8(98), negotiated.PayloadType)
		assert.Equal(t, uint8(98), negotiated.PayloadTypeRecv())
		assert.Equal(t, CodecFmtp("octet-align=1; mode-set=0,1,2"), negotiated.Fmtp)

		answer := string(sess.LocalSDP(true))
		assert.Contains(t, answer, "m=audio 1234 RTP/AVP 98\r\n")
		assert.Contains(t, answer, "a=rtpmap:98 AMR-WB/16000\r\na=fmtp:98 octet-align=1; mode-set=0,1,2\r\n")
	}
}

// TestOctetAlignSelectsAMRWBVariant checks that octet-align, which selects the
// RFC 4867 payload format, takes part in matching with its default 0: the
// answer is paired with the local variant of the same format, whose payload
// type becomes the receive one.
func TestOctetAlignSelectsAMRWBVariant(t *testing.T) {
	for _, tt := range []struct {
		name     string
		local    []Codec
		fmtp     []string
		wantRecv uint8
		wantErr  bool
	}{
		{name: "octet-aligned", local: []Codec{codecAMRWB, codecAMRWBOctet}, fmtp: []string{"fmtp:98 octet-align=1"}, wantRecv: 119},
		{name: "explicit bandwidth-efficient", local: []Codec{codecAMRWBOctet, codecAMRWB}, fmtp: []string{"fmtp:98 octet-align=0"}, wantRecv: 118},
		{name: "absent is bandwidth-efficient", local: []Codec{codecAMRWBOctet, codecAMRWB}, wantRecv: 118},
		{name: "absent does not match octet-aligned", local: []Codec{codecAMRWBOctet}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sess := fmtpSession(tt.local...)
			sess.LocalSDP(false)
			err := sess.RemoteSDP(fmtpSDP("98", append([]string{"rtpmap:98 AMR-WB/16000"}, tt.fmtp...)...), true)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			negotiated := CodecAudioFromSession(sess)
			assert.Equal(t, uint8(98), negotiated.PayloadType)
			assert.Equal(t, tt.wantRecv, negotiated.PayloadTypeRecv())
		})
	}
}

// TestRegisterCodecFmtpMatcherForUnknownEncoding checks that a matcher registered
// for an encoding unknown to diago selects the local variant with the same
// payload format, case-insensitively, and that removing it restores matching
// regardless of fmtp.
func TestRegisterCodecFmtpMatcherForUnknownEncoding(t *testing.T) {
	RegisterCodecFmtpMatcher("x-framed", func(fmtp, other CodecFmtp) bool {
		v, ok := fmtp.Get("framing")
		otherV, otherOk := other.Get("framing")
		return ok == otherOk && v == otherV
	})
	t.Cleanup(func() { RegisterCodecFmtpMatcher("x-framed", nil) })

	framedA := Codec{Name: "X-FRAMED", PayloadType: 110, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1}
	framedB := Codec{Name: "X-FRAMED", PayloadType: 111, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Fmtp: "framing=b"}
	remote := fmtpSDP("98", "rtpmap:98 x-framed/8000", "fmtp:98 framing=b")

	sess := fmtpSession(framedA, framedB)
	sess.LocalSDP(false)
	require.NoError(t, sess.RemoteSDP(remote, true))
	assert.Equal(t, uint8(111), CodecAudioFromSession(sess).PayloadTypeRecv())

	require.Error(t, fmtpSession(framedA).RemoteSDP(remote, false))

	RegisterCodecFmtpMatcher("X-Framed", nil)
	require.NoError(t, fmtpSession(framedA).RemoteSDP(remote, false))
}

func TestCodecFmtpWith(t *testing.T) {
	for _, tt := range []struct {
		name       string
		fmtp       CodecFmtp
		key, value string
		want       CodecFmtp
	}{
		{name: "empty", key: "octet-align", value: "1", want: "octet-align=1"},
		{name: "append", fmtp: "mode-set=0,1,2", key: "octet-align", value: "1", want: "mode-set=0,1,2; octet-align=1"},
		{name: "replace case-insensitive keeps order", fmtp: "Octet-Align=0;mode-set=0,1,2", key: "octet-align", value: "1", want: "octet-align=1; mode-set=0,1,2"},
		{name: "replace only first", fmtp: "a=1; a=2", key: "a", value: "3", want: "a=3; a=2"},
		{name: "empty value writes bare key", fmtp: "a=1;", key: "b", want: "a=1; b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.fmtp.With(tt.key, tt.value)
			assert.Equal(t, tt.want, f)
			v, ok := f.Get(tt.key)
			assert.True(t, ok)
			assert.Equal(t, tt.value, v)
		})
	}
}

func TestDifferentEncodingOnSamePayloadTypeDoesNotMatch(t *testing.T) {
	sess := fmtpSession(codecAMRWB)
	require.Error(t, sess.RemoteSDP(fmtpSDP("118", "rtpmap:118 AMR/8000"), false))
}

func TestStaticPayloadTypesWithoutRtpmap(t *testing.T) {
	codecs := make([]Codec, 2)
	n, err := CodecsFromSDPRead([]string{"9", "18"}, []string{"fmtp:18 annexb=no"}, codecs)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	assert.Equal(t, CodecAudioG722, codecs[0])
	g729 := CodecAudioG729
	g729.Fmtp = "annexb=no"
	assert.Equal(t, g729, codecs[1])
}

func TestCodecsFromSDPReadSkipsPayloadTypesOutsideRTPRange(t *testing.T) {
	codecs := make([]Codec, 3)
	n, _ := CodecsFromSDPRead([]string{"127", "128", "200"}, []string{
		"rtpmap:127 opus/48000/2", "rtpmap:128 opus/48000/2", "rtpmap:200 opus/48000/2",
	}, codecs)
	require.Equal(t, 1, n)
	assert.Equal(t, uint8(127), codecs[0].PayloadType)
}
