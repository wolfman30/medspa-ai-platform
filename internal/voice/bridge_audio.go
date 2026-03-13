package voice

// ──────────────────────────────────────────────────────────────────────────────
// Mu-law ↔ Linear16 conversion (ITU-T G.711)
// ──────────────────────────────────────────────────────────────────────────────

// convertInputAudio converts audio from the Telnyx encoding to Linear16 for Nova Sonic.
func (b *Bridge) convertInputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm", "L16", "l16":
		return audio, nil
	case "audio/x-mulaw":
		return mulawToLinear16(audio), nil
	default:
		b.logger.Warn("bridge: unknown input encoding, passing through", "encoding", b.mediaFormat.Encoding)
		return audio, nil
	}
}

// convertOutputAudio converts audio from Linear16 (Nova Sonic) to the Telnyx encoding.
func (b *Bridge) convertOutputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm", "L16", "l16":
		return audio, nil
	case "audio/x-mulaw":
		return linear16ToMulaw(audio), nil
	default:
		return audio, nil
	}
}

// mulawToLinear16 decodes mu-law encoded audio to 16-bit linear PCM.
func mulawToLinear16(mulaw []byte) []byte {
	linear := make([]byte, len(mulaw)*2)
	for i, b := range mulaw {
		sample := mulawDecodeTable[b]
		linear[i*2] = byte(sample & 0xFF)
		linear[i*2+1] = byte((sample >> 8) & 0xFF)
	}
	return linear
}

// linear16ToMulaw encodes 16-bit linear PCM audio to mu-law.
func linear16ToMulaw(linear []byte) []byte {
	n := len(linear) / 2
	mulaw := make([]byte, n)
	for i := 0; i < n; i++ {
		sample := int16(linear[i*2]) | int16(linear[i*2+1])<<8
		mulaw[i] = linearToMulawSample(sample)
	}
	return mulaw
}

// linearToMulawSample converts a single 16-bit linear PCM sample to mu-law.
func linearToMulawSample(sample int16) byte {
	const (
		mulawMax  = 0x1FFF
		mulawBias = 33
	)

	sign := byte(0)
	if sample < 0 {
		sign = 0x80
		sample = -sample
	}

	if int(sample) > mulawMax {
		sample = mulawMax
	}
	sample += mulawBias

	exp := byte(7)
	for expMask := int16(0x4000); (sample & expMask) == 0; expMask >>= 1 {
		if exp == 0 {
			break
		}
		exp--
	}

	mantissa := byte((sample >> (exp + 3)) & 0x0F)
	encoded := ^(sign | (exp << 4) | mantissa)
	return encoded
}

// mulawDecodeTable is the ITU-T G.711 mu-law to linear16 lookup table.
var mulawDecodeTable = [256]int16{
	-32124, -31100, -30076, -29052, -28028, -27004, -25980, -24956,
	-23932, -22908, -21884, -20860, -19836, -18812, -17788, -16764,
	-15996, -15484, -14972, -14460, -13948, -13436, -12924, -12412,
	-11900, -11388, -10876, -10364, -9852, -9340, -8828, -8316,
	-7932, -7676, -7420, -7164, -6908, -6652, -6396, -6140,
	-5884, -5628, -5372, -5116, -4860, -4604, -4348, -4092,
	-3900, -3772, -3644, -3516, -3388, -3260, -3132, -3004,
	-2876, -2748, -2620, -2492, -2364, -2236, -2108, -1980,
	-1884, -1820, -1756, -1692, -1628, -1564, -1500, -1436,
	-1372, -1308, -1244, -1180, -1116, -1052, -988, -924,
	-876, -844, -812, -780, -748, -716, -684, -652,
	-620, -588, -556, -524, -492, -460, -428, -396,
	-372, -356, -340, -324, -308, -292, -276, -260,
	-244, -228, -212, -196, -180, -164, -148, -132,
	-120, -112, -104, -96, -88, -80, -72, -64,
	-56, -48, -40, -32, -24, -16, -8, 0,
	32124, 31100, 30076, 29052, 28028, 27004, 25980, 24956,
	23932, 22908, 21884, 20860, 19836, 18812, 17788, 16764,
	15996, 15484, 14972, 14460, 13948, 13436, 12924, 12412,
	11900, 11388, 10876, 10364, 9852, 9340, 8828, 8316,
	7932, 7676, 7420, 7164, 6908, 6652, 6396, 6140,
	5884, 5628, 5372, 5116, 4860, 4604, 4348, 4092,
	3900, 3772, 3644, 3516, 3388, 3260, 3132, 3004,
	2876, 2748, 2620, 2492, 2364, 2236, 2108, 1980,
	1884, 1820, 1756, 1692, 1628, 1564, 1500, 1436,
	1372, 1308, 1244, 1180, 1116, 1052, 988, 924,
	876, 844, 812, 780, 748, 716, 684, 652,
	620, 588, 556, 524, 492, 460, 428, 396,
	372, 356, 340, 324, 308, 292, 276, 260,
	244, 228, 212, 196, 180, 164, 148, 132,
	120, 112, 104, 96, 88, 80, 72, 64,
	56, 48, 40, 32, 24, 16, 8, 0,
}
