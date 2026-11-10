package main

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type tags struct {
	title, artist, album, albumArtist string
	artwork                           bool
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
	return result
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
