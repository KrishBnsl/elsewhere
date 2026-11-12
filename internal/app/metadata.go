package app

import "math"

func nextMode(mode string) string {
	switch mode {
	case "track":
		return "album"
	case "album":
		return "off"
	default:
		return "track"
	}
}

func (p *player) trackVolume(t track) int {
	var gain *float64
	switch p.gain {
	case "track":
		gain = t.trackGain
	case "album":
		gain = t.albumGain
		if gain == nil {
			gain = t.trackGain
		}
	}
	if gain == nil {
		return p.volume
	}
	return int(math.Round(min(100, float64(p.volume)*math.Pow(10, min(30, *gain)/20))))
}
