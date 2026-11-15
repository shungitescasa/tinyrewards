package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Local struct {
	root *os.Root
}

func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

func localName(key string) (string, error) {
	name, err := filepath.Localize(key)
	if err != nil {
		return "", fmt.Errorf("invalid storage key %q: %w", key, err)
	}
	return name, nil
}

func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := localName(key)
	if err != nil {
		return err
	}

	dir := filepath.Dir(name)
	if dir != "." {
		if err := l.root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	tmp := filepath.Join(dir, "."+rand.Text()+".part")
	f, err := l.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}

	n, err := io.Copy(f, r)
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("wrote %d bytes, expected %d", n, size)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = l.root.Rename(tmp, name)
	}
	if err != nil {
		l.root.Remove(tmp)
		return err
	}
	return nil
}

func (l *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	name, err := localName(key)
	if err != nil {
		return nil, err
	}
	return l.root.Open(name)
}

func (l *Local) Delete(ctx context.Context, key string) error {
	name, err := localName(key)
	if err != nil {
		return err
	}
	if err := l.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
