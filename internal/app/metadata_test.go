package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestTaggedFLACAlbumAcrossFolders(t *testing.T) {
	root := t.TempDir()
	for i, artist := range []string{"One Artist", "Another Artist"} {
		folder := filepath.Join(root, []string{"Disc 1", "Disc 2"}[i])
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
		var comments bytes.Buffer
		field := func(value string) {
			comments.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(value))))
			comments.WriteString(value)
		}
		field("test encoder")
		comments.Write(binary.LittleEndian.AppendUint32(nil, 4))
		field("title=Track")
		field("ARTIST=" + artist)
		field("ALBUM=An Album")
		field("ALBUMARTIST=Various Artists")
		size := comments.Len()
		contents := append([]byte("fLaC\x00\x00\x00\x22"), make([]byte, 34)...)
		contents = append(contents, 0x84, byte(size>>16), byte(size>>8), byte(size))
		contents = append(contents, comments.Bytes()...)
		if err := os.WriteFile(filepath.Join(folder, "01.flac"), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	albums, err := scanLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || len(albums[0].tracks) != 2 {
		t.Fatalf("expected one album with two tracks, got %+v", albums)
	}
	album := albums[0]
	if album.title != "An Album" || album.artist != "Various Artists" ||
		album.tracks[0].title != "Track" || album.tracks[1].artist != "Another Artist" {
		t.Fatalf("unexpected metadata: %+v", album)
	}
}
