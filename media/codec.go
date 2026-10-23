// SPDX-License-Identifier: MPL-2.0
// SPDX-FileCopyrightText: Copyright (c) 2024, Emir Aganovic

package media

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/diago/media/sdp"
)

var (
	// Here are some codec constants that can be reused
	CodecAudioUlaw          = Codec{PayloadType: 0, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Name: "PCMU"}
	CodecAudioAlaw          = Codec{PayloadType: 8, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Name: "PCMA"}
	CodecAudioOpus          = Codec{PayloadType: 96, SampleRate: 48000, SampleDur: 20 * time.Millisecond, NumChannels: 2, Name: "opus"}
	CodecTelephoneEvent8000 = Codec{PayloadType: 101, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Name: "telephone-event"}
	// CodecComfortNoise8000 describes the static RFC 3389 comfort-noise payload.
	// It is an auxiliary RTP format and is not included in default SDP offers.
	CodecComfortNoise8000 = Codec{PayloadType: 13, SampleRate: 8000, NumChannels: 1, Name: "CN"}
)

type Codec struct {
	Name        string
	PayloadType uint8
	SampleRate  uint32
	SampleDur   time.Duration
	NumChannels int // 1 or 2
	// Fmtp holds the format parameters of the codec (the value of
	// a=fmtp:<payload type>), for example "octet-align=1" for AMR-WB.
	// It is parsed from remote SDP and written to local SDP.
	Fmtp string
}

func (c *Codec) String() string {
	return fmt.Sprintf("name=%s pt=%d rate=%d dur=%s channels=%d fmtp=%s", c.Name, c.PayloadType, c.SampleRate, c.SampleDur.String(), c.NumChannels, c.Fmtp)
}

// staticAudioCodecs are the RFC 3551 static payload types that an SDP may
// list without an a=rtpmap attribute.
var staticAudioCodecs = map[string]Codec{
	"0":  CodecAudioUlaw,
	"8":  CodecAudioAlaw,
	"9":  {PayloadType: 9, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Name: "G722"},
	"18": {PayloadType: 18, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1, Name: "G729"},
}

// codecsMatch reports whether a remote codec is the same encoding as a local
// one. Encodings are compared by name, clock rate and channels so that a
// dynamic payload type may differ between the two sides (RFC 3264 lets the
// offerer choose it); codecs without a name fall back to the payload type.
func codecsMatch(local, remote Codec) bool {
	if local.Name == "" || remote.Name == "" {
		return local.PayloadType == remote.PayloadType
	}
	return strings.EqualFold(local.Name, remote.Name) &&
		local.SampleRate == remote.SampleRate &&
		codecChannels(local) == codecChannels(remote)
}

func codecChannels(c Codec) int {
	if c.NumChannels <= 0 {
		return 1
	}
	return c.NumChannels
}

// SampleTimestamp returns number of samples as RTP Timestamp measure
func (c *Codec) SampleTimestamp() uint32 {
	return uint32(float64(c.SampleRate) * c.SampleDur.Seconds())
}

// Samples16 returns PCM 16 bit samples size
func (c *Codec) Samples16() int {
	return c.SamplesPCM(16)
}

// Samples is samples in pcm
func (c *Codec) SamplesPCM(bitSize int) int {
	return bitSize / 8 * int(float64(c.SampleRate)*c.SampleDur.Seconds()) * c.NumChannels
}

func CodecAudioFromSession(s *MediaSession) Codec {
	codec, exists := CodecAudioFromList(s.filterCodecs)
	if !exists {
		return s.Codecs[0]
	}

	return codec
}

// CodecTelephoneEventFromSession returns the 8 kHz telephone-event codec used
// by the session for RFC 4733 DTMF: the negotiated one, which carries the
// payload type agreed with the peer, otherwise the local one, otherwise
// CodecTelephoneEvent8000.
func CodecTelephoneEventFromSession(s *MediaSession) Codec {
	for _, codecs := range [][]Codec{s.filterCodecs, s.Codecs} {
		for _, codec := range codecs {
			if strings.EqualFold(codec.Name, CodecTelephoneEvent8000.Name) && codec.SampleRate == CodecTelephoneEvent8000.SampleRate {
				return codec
			}
		}
	}
	return CodecTelephoneEvent8000
}

// IsAuxiliary reports whether c is an auxiliary RTP format (RFC 4733 events,
// RFC 3389 comfort noise) carried beside the audio rather than decoded as it.
func (c Codec) IsAuxiliary() bool {
	return strings.EqualFold(c.Name, "telephone-event") || strings.EqualFold(c.Name, "CN")
}

func CodecAudioFromList(codecs []Codec) (Codec, bool) {
	for _, codec := range codecs {
		if codec.IsAuxiliary() {
			continue
		}

		return codec, true
	}

	return Codec{}, false
}

// Deprecated: Use CodecAudioFromSession
func CodecFromSession(s *MediaSession) Codec {
	return CodecAudioFromSession(s)
}

// Deprecated: Use CodecAudioFromPayloadType
func CodecFromPayloadType(payloadType uint8) Codec {
	f := strconv.Itoa(int(payloadType))
	return mapSupportedCodec(f)
}

func CodecAudioFromPayloadType(payloadType uint8) (Codec, error) {
	f := strconv.Itoa(int(payloadType))
	switch f {
	case sdp.FORMAT_TYPE_ALAW:
		return CodecAudioAlaw, nil
	case sdp.FORMAT_TYPE_ULAW:
		return CodecAudioUlaw, nil
	case sdp.FORMAT_TYPE_OPUS:
		return CodecAudioOpus, nil
	case sdp.FORMAT_TYPE_TELEPHONE_EVENT:
		return CodecTelephoneEvent8000, nil
	}
	return Codec{}, fmt.Errorf("non supported codec: %d", payloadType)
}

func mapSupportedCodec(f string) Codec {
	// TODO: Here we need to be more explicit like matching sample rate, channels and other

	switch f {
	case sdp.FORMAT_TYPE_ALAW:
		return CodecAudioAlaw
	case sdp.FORMAT_TYPE_ULAW:
		return CodecAudioUlaw
	case sdp.FORMAT_TYPE_OPUS:
		return CodecAudioOpus
	case sdp.FORMAT_TYPE_TELEPHONE_EVENT:
		return CodecTelephoneEvent8000
	default:
		slog.Warn("Unsupported format. Using default clock rate", "format", f)
	}
	// Format as default
	pt, err := sdp.FormatNumeric(f)
	if err != nil {
		slog.Warn("Format is non numeric value", "format", f)
	}
	return Codec{
		PayloadType: pt,
		SampleRate:  8000,
		SampleDur:   20 * time.Millisecond,
	}
}

// func CodecsFromSDP(log *slog.Logger, sd sdp.SessionDescription, codecsAudio []Codec) error {
// 	md, err := sd.MediaDescription("audio")
// 	if err != nil {
// 		return err
// 	}

// 	codecs := make([]Codec, len(md.Formats))
// 	attrs := sd.Values("a")
// 	n, err := CodecsFromSDPRead(log, md, attrs, codecs)
// 	if err != nil {
// 		return err
// 	}
// 	codecs = codecs[:n]
// }

// CodecsFromSDP will try to parse as much as possible, but it will return also error in case
// some properties could not be read
// You can take what is parsed or return error
func CodecsFromSDPRead(formats []string, attrs []string, codecsAudio []Codec) (int, error) {
	n := 0
	var rerr error
	for _, f := range formats {
		if f == "0" || f == "8" {
			codecsAudio[n] = staticAudioCodecs[f]
			n++
			continue
		}

		pt64, err := strconv.ParseUint(f, 10, 8)
		if err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("format type failed to conv to integer, skipping f=%s: %w", f, err))
			continue
		}
		// RTP payload types are 7 bits (RFC 3550).
		if pt64 > 127 {
			rerr = errors.Join(rerr, fmt.Errorf("payload type %d is out of the RTP range 0-127, skipping", pt64))
			continue
		}
		pt := uint8(pt64)

		found := false
		for _, a := range attrs {
			// a=rtpmap:<payload type> <encoding name>/<clock rate> [/<encoding parameters>]
			pref := "rtpmap:" + f + " "
			if strings.HasPrefix(a, pref) {
				// Check properties of this codec
				str := a[len(pref):]
				// TODO use more efficient reading props
				props := strings.Split(str, " ")
				firstProp := props[0]
				propsCodec := strings.Split(firstProp, "/")
				if len(propsCodec) < 2 {
					rerr = errors.Join(rerr, fmt.Errorf("bad rtmap property a=%s", a))
					continue
				}

				encodingName := propsCodec[0]
				sampleRateStr := propsCodec[1]
				sampleRate64, err := strconv.ParseUint(sampleRateStr, 10, 32)
				if err != nil {
					rerr = errors.Join(rerr, fmt.Errorf("sample rate failed to parse a=%s: %w", a, err))
					continue
				}

				codec := Codec{
					Name:        encodingName,
					PayloadType: pt,
					SampleRate:  uint32(sampleRate64),
					// TODO check ptime ?
					SampleDur:   20 * time.Millisecond,
					NumChannels: 1,
				}

				if len(propsCodec) == 3 {
					numChannels, err := strconv.ParseUint(propsCodec[2], 10, 32)
					if err == nil {
						codec.NumChannels = int(numChannels)
					}
				}
				codec.Fmtp = formatParameters(f, attrs)
				codecsAudio[n] = codec
				n++
				found = true
			}
		}
		if static, ok := staticAudioCodecs[f]; ok && !found {
			static.Fmtp = formatParameters(f, attrs)
			codecsAudio[n] = static
			n++
		}
	}
	return n, nil
}

// formatParameters returns the value of a=fmtp:<format>, or "" if absent.
func formatParameters(format string, attrs []string) string {
	pref := "fmtp:" + format + " "
	for _, a := range attrs {
		if strings.HasPrefix(a, pref) {
			return strings.TrimSpace(a[len(pref):])
		}
	}
	return ""
}
