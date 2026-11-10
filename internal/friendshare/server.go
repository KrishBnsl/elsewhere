package friendshare

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
)

type account struct {
	Person
	Friends      []string
	Inbox        []Card
	TasteEnabled bool
	Taste        *Taste
}

type invitation struct {
	Owner   string
	Expires time.Time
}

type database struct {
	Accounts map[string]account
	Invites  map[string]invitation
	Jams     map[string]Jam
}

type service struct {
	setup [32]byte
	store stateStore
}

type stateChange func(database) (*database, Reply, int)

type stateStore interface {
	apply(context.Context, bool, stateChange) (Reply, int, error)
	ready(context.Context) error
}

func newService(setup string, store stateStore) (*service, error) {
	if len(setup) < 20 {
		return nil, errors.New("setup key must have at least 20 characters")
	}
	return &service{setup: sha256.Sum256([]byte(setup)), store: store}, nil
}

func emptyDatabase() database {
	return database{Accounts: make(map[string]account), Invites: make(map[string]invitation), Jams: make(map[string]Jam)}
}

func decodeDatabase(data []byte) (database, error) {
	var state database
	if err := json.Unmarshal(data, &state); err != nil {
		return database{}, err
	}
	if state.Accounts == nil || state.Invites == nil {
		return database{}, errors.New("invalid friends database")
	}
	return state, nil
}

func secretKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func validText(s string, limit int) bool {
	return len(s) <= limit && strings.IndexFunc(s, unicode.IsControl) == -1
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/readyz" && r.Method == http.MethodGet {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := s.store.ready(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(Reply{Error: "friends storage unavailable"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	reply, code := s.request(w, r)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(reply)
}

func (s *service) request(w http.ResponseWriter, r *http.Request) (Reply, int) {
	fail := func(status int, message string) (Reply, int) { return Reply{Error: message}, status }
	if r.Method != http.MethodPost {
		return fail(http.StatusMethodNotAllowed, "use POST")
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(token) < 25 || len(token) > 128 {
		return fail(http.StatusUnauthorized, "missing device credential")
	}
	var req Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF {
		return fail(http.StatusBadRequest, "invalid request")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	path := r.URL.Path
	reply, code, err := s.store.apply(ctx, path != "/v1/home", func(state database) (*database, Reply, int) {
		return s.change(state, path, token, req)
	})
	if err != nil {
		return fail(http.StatusServiceUnavailable, "friends storage unavailable")
	}
	return reply, code
}

func (s *service) change(state database, path, token string, req Request) (*database, Reply, int) {
	fail := func(status int, message string) (*database, Reply, int) { return nil, Reply{Error: message}, status }
	key := secretKey(token)
	me, known := state.Accounts[key]
	if !known && path != "/v1/connect" && path != "/v1/accept" {
		return fail(http.StatusUnauthorized, "connect or accept an invite first")
	}
	if path == "/v1/home" {
		return nil, home(state, me), http.StatusOK
	}
	// Copy on write so a failed persistence operation leaves the live state intact.
	db := database{Accounts: make(map[string]account, len(state.Accounts)+1), Invites: make(map[string]invitation), Jams: make(map[string]Jam)}
	for id, a := range state.Accounts {
		db.Accounts[id] = a
	}
	now := time.Now().UTC()
	for id, jam := range state.Jams {
		db.Jams[id] = jam
	}
	for id, inv := range state.Invites {
		if inv.Expires.After(now) {
			db.Invites[id] = inv
		}
	}
	name := strings.TrimSpace(req.Name)
	newAccount := func() bool {
		if name == "" || !validText(name, 64) || len(db.Accounts) >= 100 {
			return false
		}
		me = account{Person: Person{ID: rand.Text(), Name: name}}
		return true
	}
	var reply Reply
	switch path {
	case "/v1/connect":
		setup := sha256.Sum256([]byte(req.Code))
		if subtle.ConstantTimeCompare(setup[:], s.setup[:]) != 1 {
			return fail(http.StatusForbidden, "incorrect setup key")
		}
		if !known && !newAccount() {
			return fail(http.StatusBadRequest, "invalid display name or service full")
		}
	case "/v1/invite":
		for id, inv := range db.Invites {
			if inv.Owner == key {
				delete(db.Invites, id)
			}
		}
		reply.Code = rand.Text()
		db.Invites[secretKey(reply.Code)] = invitation{Owner: key, Expires: now.Add(24 * time.Hour)}
	case "/v1/accept":
		inviteKey := secretKey(req.Code)
		inv, exists := db.Invites[inviteKey]
		if !exists || inv.Owner == key {
			return fail(http.StatusBadRequest, "invalid, expired, or self-issued invite")
		}
		if !known && !newAccount() {
			return fail(http.StatusBadRequest, "invalid display name or service full")
		}
		friend := db.Accounts[inv.Owner]
		if !slices.Contains(me.Friends, inv.Owner) {
			me.Friends = append(slices.Clone(me.Friends), inv.Owner)
			friend.Friends = append(slices.Clone(friend.Friends), key)
		}
		db.Accounts[inv.Owner] = friend
		delete(db.Invites, inviteKey)
	case "/v1/send":
		song := req.Song
		if !validSong(song) {
			return fail(http.StatusBadRequest, "invalid song card")
		}
		recipient, friend := findFriend(db, me, req.Friend)
		if friend == "" {
			return fail(http.StatusForbidden, "recipient is not connected")
		}
		inbox := recipient.Inbox[max(0, len(recipient.Inbox)-99):]
		recipient.Inbox = append(slices.Clone(inbox), Card{ID: rand.Text(), From: me.Person, Song: song, At: now})
		db.Accounts[friend] = recipient
	case "/v1/disconnect":
		recipient, friend := findFriend(db, me, req.Friend)
		if friend == "" {
			return fail(http.StatusBadRequest, "friend is not connected")
		}
		me.Friends = slices.DeleteFunc(slices.Clone(me.Friends), func(id string) bool { return id == friend })
		recipient.Friends = slices.DeleteFunc(slices.Clone(recipient.Friends), func(id string) bool { return id == key })
		me.Inbox = slices.DeleteFunc(slices.Clone(me.Inbox), func(card Card) bool { return card.From.ID == recipient.ID })
		recipient.Inbox = slices.DeleteFunc(slices.Clone(recipient.Inbox), func(card Card) bool { return card.From.ID == me.ID })
		db.Accounts[friend] = recipient
		if id, jam := jamFor(db, me.ID); id != "" && (jam.Host.ID == recipient.ID || jam.Guest.ID == recipient.ID) {
			delete(db.Jams, id)
		}
	case "/v1/taste-on":
		me.TasteEnabled = true
	case "/v1/taste-off":
		me.TasteEnabled, me.Taste = false, nil
	case "/v1/taste-update":
		if !me.TasteEnabled || !validTaste(req.Taste) {
			return fail(http.StatusBadRequest, "enable taste sharing or send a supported summary")
		}
		me.Taste = req.Taste
	default:
		if !strings.HasPrefix(path, "/v1/jam-") {
			return fail(http.StatusNotFound, "unknown friends endpoint")
		}
		if err := changeJam(&db, me, strings.TrimPrefix(path, "/v1/jam-"), req, now.UnixMilli()); err != nil {
			return fail(http.StatusConflict, err.Error())
		}
	}
	db.Accounts[key] = me
	result := home(db, me)
	result.Code = reply.Code
	return &db, result, http.StatusOK
}

func findFriend(db database, me account, publicID string) (account, string) {
	for _, id := range me.Friends {
		if a, ok := db.Accounts[id]; ok && a.ID == publicID {
			return a, id
		}
	}
	return account{}, ""
}

func home(db database, me account) Reply {
	reply := Reply{Me: me.Person, Cards: me.Inbox, TasteEnabled: me.TasteEnabled, Now: time.Now().UnixMilli()}
	if id, jam := jamFor(db, me.ID); id != "" {
		reply.Jam = &jam
	}
	for _, id := range me.Friends {
		if a, ok := db.Accounts[id]; ok {
			reply.Friends = append(reply.Friends, a.Person)
			if a.TasteEnabled && a.Taste != nil {
				reply.Tastes = append(reply.Tastes, SharedTaste{Person: a.Person, Taste: *a.Taste})
			}
		}
	}
	return reply
}

func validSong(song Song) bool {
	hash, err := hex.DecodeString(song.Hash)
	return err == nil && len(hash) == 8 && strings.ToLower(song.Hash) == song.Hash &&
		strings.TrimSpace(song.Title) != "" && validText(song.Title, 256) &&
		validText(song.Artist, 256) && validText(song.Album, 256)
}

func validTaste(t *Taste) bool {
	if t == nil || t.Version == 0 || len(t.Regions) > 3 {
		return false
	}
	for _, r := range t.Regions {
		if r.Weight <= 0 || r.Weight > 1 || len(r.Sound) == 0 || len(r.Sound) > 256 {
			return false
		}
		for _, v := range r.Sound {
			if v < -1e12 || v > 1e12 {
				return false
			}
		}
	}
	return true
}
