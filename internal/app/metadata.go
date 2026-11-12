package app

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type tags struct {
	title, artist, album, albumArtist string
	artwork                           bool
	disc, number                      int
	trackGain, albumGain              *float64
}

func readMetadata(path string) tags {
	result := readNativeMetadata(path)
	if !strings.EqualFold(filepath.Ext(path), ".flac") {
		return result
	}
	// AVFoundation can omit Vorbis comments from commonMetadata for local FLAC files.
	flac := readFLACTags(path)
	for _, field := range []struct {
		value string
		dest  *string
	}{
		{flac.title, &result.title}, {flac.artist, &result.artist},
		{flac.album, &result.album}, {flac.albumArtist, &result.albumArtist},
	} {
		if field.value != "" {
			*field.dest = field.value
		}
	}
	if flac.disc != 0 {
		result.disc = flac.disc
	}
	if flac.number != 0 {
		result.number = flac.number
	}
	if flac.trackGain != nil {
		result.trackGain = flac.trackGain
	}
	if flac.albumGain != nil {
		result.albumGain = flac.albumGain
	}
	return result
}

func tagNumber(value string) int {
	first, _, _ := strings.Cut(value, "/")
	n, _ := strconv.Atoi(strings.TrimSpace(first))
	return max(0, n)
}

func tagGain(value string) *float64 {
	db, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.ToLower(value), "db")), 64)
	if err != nil || math.IsNaN(db) || math.IsInf(db, 0) {
		return nil
	}
	return &db
}

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

func readFLACTags(path string) tags {
	file, err := os.Open(path)
	if err != nil {
		return tags{}
	}
	defer file.Close()

	var header [4]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || string(header[:]) != "fLaC" {
		return tags{}
	}
	for {
		if _, err := io.ReadFull(file, header[:]); err != nil {
			return tags{}
		}
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		if header[0]&0x7f == 4 {
			block := make([]byte, length)
			if _, err := io.ReadFull(file, block); err != nil {
				return tags{}
			}
			return vorbisTags(block)
		}
		if header[0]&0x80 != 0 {
			return tags{}
		}
		if _, err := file.Seek(int64(length), io.SeekCurrent); err != nil {
			return tags{}
		}
	}
}

func vorbisTags(block []byte) (result tags) {
	readField := func() ([]byte, bool) {
		if len(block) < 4 {
			return nil, false
		}
		length := binary.LittleEndian.Uint32(block)
		block = block[4:]
		if length > uint32(len(block)) {
			return nil, false
		}
		field := block[:length]
		block = block[length:]
		return field, true
	}
	if _, ok := readField(); !ok || len(block) < 4 {
		return result
	}
	count := binary.LittleEndian.Uint32(block)
	block = block[4:]
	for range count {
		field, ok := readField()
		if !ok {
			break
		}
		key, value, ok := strings.Cut(string(field), "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		var dest *string
		switch strings.ToUpper(key) {
		case "DISCNUMBER":
			result.disc = tagNumber(value)
		case "TRACKNUMBER":
			result.number = tagNumber(value)
		case "REPLAYGAIN_TRACK_GAIN":
			result.trackGain = tagGain(value)
		case "REPLAYGAIN_ALBUM_GAIN":
			result.albumGain = tagGain(value)
		case "TITLE":
			dest = &result.title
		case "ARTIST":
			dest = &result.artist
		case "ALBUM":
			dest = &result.album
		case "ALBUMARTIST", "ALBUM ARTIST":
			dest = &result.albumArtist
		}
		if dest != nil && *dest == "" {
			*dest = value
		}
	}
	return result
}
