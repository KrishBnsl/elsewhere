package audio

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Tags are the fields read from an audio file's metadata.
type Tags struct {
	Title, Artist, Album, AlbumArtist string
	Artwork                           bool
	Disc, Number                      int
	TrackGain, AlbumGain              *float64
}

// ReadMetadata reads a file's tags, adding Vorbis comments that AVFoundation misses in FLAC.
func ReadMetadata(path string) Tags {
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
		{flac.Title, &result.Title}, {flac.Artist, &result.Artist},
		{flac.Album, &result.Album}, {flac.AlbumArtist, &result.AlbumArtist},
	} {
		if field.value != "" {
			*field.dest = field.value
		}
	}
	if flac.Disc != 0 {
		result.Disc = flac.Disc
	}
	if flac.Number != 0 {
		result.Number = flac.Number
	}
	if flac.TrackGain != nil {
		result.TrackGain = flac.TrackGain
	}
	if flac.AlbumGain != nil {
		result.AlbumGain = flac.AlbumGain
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

func readFLACTags(path string) Tags {
	file, err := os.Open(path)
	if err != nil {
		return Tags{}
	}
	defer file.Close()

	var header [4]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || string(header[:]) != "fLaC" {
		return Tags{}
	}
	for {
		if _, err := io.ReadFull(file, header[:]); err != nil {
			return Tags{}
		}
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		if header[0]&0x7f == 4 {
			block := make([]byte, length)
			if _, err := io.ReadFull(file, block); err != nil {
				return Tags{}
			}
			return vorbisTags(block)
		}
		if header[0]&0x80 != 0 {
			return Tags{}
		}
		if _, err := file.Seek(int64(length), io.SeekCurrent); err != nil {
			return Tags{}
		}
	}
}

func vorbisTags(block []byte) (result Tags) {
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
			result.Disc = tagNumber(value)
		case "TRACKNUMBER":
			result.Number = tagNumber(value)
		case "REPLAYGAIN_TRACK_GAIN":
			result.TrackGain = tagGain(value)
		case "REPLAYGAIN_ALBUM_GAIN":
			result.AlbumGain = tagGain(value)
		case "TITLE":
			dest = &result.Title
		case "ARTIST":
			dest = &result.Artist
		case "ALBUM":
			dest = &result.Album
		case "ALBUMARTIST", "ALBUM ARTIST":
			dest = &result.AlbumArtist
		}
		if dest != nil && *dest == "" {
			*dest = value
		}
	}
	return result
}
