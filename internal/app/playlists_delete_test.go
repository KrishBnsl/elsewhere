package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeletePlaylistThroughPicker(t *testing.T) {
	p, a, _ := queueFixture(t)
	p.playlistPath = filepath.Join(t.TempDir(), "playlists.json")
	tr := a.tracks[0]
	lists, err := editPlaylists(p.playlistPath, func(lists *[]playlist) error {
		*lists = []playlist{
			{Name: "Keep", Tracks: []string{tr.likeID}},
			{Name: "Remove", Tracks: []string{tr.likeID}},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p.playlistRecords = lists
	p.rebuildPlaylists()
	if !p.runSlashCommand("dp", searchPrompt{}) {
		t.Fatal("/dp was not recognized")
	}
	p.handlePlaylist("remove")
	p.handlePlaylist("\r")
	if p.playlistDialog.deleteTarget == nil || p.playlistDialog.deleteTarget.Name != "Remove" {
		t.Fatal("picker did not select the matching playlist")
	}
	p.handlePlaylist("\x1b")
	if p.playlistDialog == nil || p.playlistDialog.deleteTarget != nil || len(p.playlistRecords) != 2 {
		t.Fatal("cancel did not return to the picker without deleting")
	}
	p.handlePlaylist("\r")
	p.handlePlaylist("\r")
	if p.playlistDialog != nil || p.notice != "Deleted playlist Remove" {
		t.Fatalf("delete did not complete: %q", p.notice)
	}
	got, err := readPlaylists(p.playlistPath)
	if err != nil || len(got) != 1 || got[0].Name != "Keep" || len(got[0].Tracks) != 1 {
		t.Fatalf("unexpected saved playlists: %+v, %v", got, err)
	}
	if _, err := os.Stat(tr.path); err != nil {
		t.Fatalf("the audio file was affected: %v", err)
	}
}
