package friendshare

import "time"

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Song struct {
	Hash   string `json:"hash"`
	Title  string `json:"title"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
}

type Card struct {
	ID   string    `json:"id"`
	From Person    `json:"from"`
	Song Song      `json:"song"`
	At   time.Time `json:"at"`
}

type Request struct {
	Name     string   `json:"name,omitempty"`
	Code     string   `json:"code,omitempty"`
	Friend   string   `json:"friend,omitempty"`
	Song     Song     `json:"song,omitzero"`
	Taste    *Taste   `json:"taste,omitempty"`
	Duration int64    `json:"duration_ms,omitempty"`
	Position int64    `json:"position_ms,omitempty"`
	Revision uint64   `json:"revision,omitempty"`
	Ready    []string `json:"ready,omitempty"`
}

type Reply struct {
	Me           Person        `json:"me,omitzero"`
	Friends      []Person      `json:"friends,omitempty"`
	Cards        []Card        `json:"cards,omitempty"`
	Code         string        `json:"code,omitempty"`
	Error        string        `json:"error,omitempty"`
	TasteEnabled bool          `json:"taste_enabled,omitempty"`
	Tastes       []SharedTaste `json:"tastes,omitempty"`
	Jam          *Jam          `json:"jam,omitempty"`
	Now          int64         `json:"now_ms"`
}

// Taste uses raw DSP descriptors before each library applies its own normalization.
type Taste struct {
	Version uint32   `json:"version"`
	Regions []Region `json:"regions"`
}

type Region struct {
	Weight float32   `json:"weight"`
	Sound  []float32 `json:"sound"`
}

type SharedTaste struct {
	Person
	Taste Taste `json:"taste"`
}

type JamTrack struct {
	ID       string `json:"id"`
	Song     Song   `json:"song"`
	Duration int64  `json:"duration_ms"`
	AddedBy  string `json:"added_by,omitempty"`
}

type Jam struct {
	ID        string              `json:"id"`
	Host      Person              `json:"host"`
	Guest     Person              `json:"guest"`
	Joined    bool                `json:"joined"`
	Revision  uint64              `json:"revision"`
	Queue     []JamTrack          `json:"queue"`
	Ready     map[string][]string `json:"ready"`
	Playing   bool                `json:"playing"`
	Position  int64               `json:"position_ms"`
	StartedAt int64               `json:"started_at_ms"`
}

func (j Jam) PositionAt(serverMS int64) int64 {
	if len(j.Queue) == 0 {
		return 0
	}
	position := j.Position
	if j.Playing {
		position += max(0, serverMS-j.StartedAt)
	}
	return min(j.Queue[0].Duration, position)
}
