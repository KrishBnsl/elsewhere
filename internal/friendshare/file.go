package friendshare

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type fileStore struct {
	mu    sync.Mutex
	path  string
	state database
}

func Open(file, setup string) (http.Handler, error) {
	store := &fileStore{path: file, state: emptyDatabase()}
	s, err := newService(setup, store)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(file)
	if err == nil {
		store.state, err = decodeDatabase(data)
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (*fileStore) ready(context.Context) error { return nil }

func (f *fileStore) apply(ctx context.Context, _ bool, change stateChange) (Reply, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Reply{}, 0, err
	}
	next, reply, code := change(f.state)
	if next != nil {
		if err := f.save(*next); err != nil {
			return Reply{}, 0, err
		}
		f.state = *next
	}
	return reply, code, nil
}

func (f *fileStore) save(state database) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(f.path), ".friends-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), f.path)
}
