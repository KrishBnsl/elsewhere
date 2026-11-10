package friendshare

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MaxJamAudio = 32 << 20

func JamAudioExtension(ext string) bool {
	switch ext {
	case ".mp3", ".m4a", ".aac", ".flac", ".wav", ".aif", ".aiff", ".caf", ".alac":
		return true
	}
	return false
}

type jamAudio struct {
	ext     string
	data    []byte
	expires time.Time
}

type jamAudioCache struct {
	sync.Mutex
	clips     map[string]*jamAudio
	uploading int
}

func (s *service) serveJamAudio(w http.ResponseWriter, r *http.Request) {
	upload := r.Method == http.MethodPut
	if !upload && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(token) < 25 || len(token) > 128 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/jam-audio/")
	authorize := func() int {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, status, err := s.store.apply(ctx, false, func(db database) (*database, Reply, int) {
			me, known := db.Accounts[secretKey(token)]
			if !known {
				return nil, Reply{}, http.StatusUnauthorized
			}
			_, room := jamFor(db, me.ID)
			if !room.Joined || len(room.Queue) == 0 || room.Queue[0].ID != id {
				return nil, Reply{}, http.StatusNotFound
			}
			owner := room.Queue[0].AddedBy
			peer := room.Host.ID
			if owner == peer {
				peer = room.Guest.ID
			}
			if owner == "" || upload != (owner == me.ID) ||
				(upload && (!slices.Contains(room.Ready[owner], id) || slices.Contains(room.Ready[peer], id))) {
				return nil, Reply{}, http.StatusForbidden
			}
			return nil, Reply{}, http.StatusOK
		})
		if err != nil {
			return http.StatusServiceUnavailable
		}
		return status
	}
	if status := authorize(); status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	cache := &s.audio
	if !upload {
		cache.Lock()
		clip := cache.clips[id]
		cache.Unlock()
		if clip == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Minute))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Jam-Audio-Extension", clip.ext)
		w.Header().Set("Content-Length", strconv.Itoa(len(clip.data)))
		_, _ = w.Write(clip.data)
		return
	}
	ext := r.Header.Get("X-Jam-Audio-Extension")
	if !JamAudioExtension(ext) || r.ContentLength <= 0 || r.ContentLength > MaxJamAudio {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	cache.Lock()
	if cache.clips[id] != nil {
		cache.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(cache.clips)+cache.uploading >= 4 {
		cache.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	cache.uploading++
	cache.Unlock()
	defer func() {
		cache.Lock()
		cache.uploading--
		cache.Unlock()
	}()
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(2 * time.Minute)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxJamAudio))
	if err != nil || int64(len(data)) != r.ContentLength {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if status := authorize(); status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	expires := time.Now().Add(2 * time.Minute)
	clip := &jamAudio{ext: ext, data: data, expires: expires}
	cache.Lock()
	if cache.clips == nil {
		cache.clips = make(map[string]*jamAudio)
	}
	cache.clips[id] = clip
	cache.Unlock()
	time.AfterFunc(2*time.Minute, func() {
		cache.Lock()
		if current := cache.clips[id]; current != nil && current.expires == expires {
			delete(cache.clips, id)
		}
		cache.Unlock()
	})
	w.WriteHeader(http.StatusNoContent)
}
