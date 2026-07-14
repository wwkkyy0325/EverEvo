package memory

import (
	"context"
	"strings"
)

// kvAdapter adapts memory.Store's meta key-value table to the core.Store
// interface, using composite keys (bucket + "/" + key) stored as string
// key-value pairs.
type kvAdapter struct {
	store *Store
}

// Get reads a value from the meta table.
func (a *kvAdapter) Get(_ context.Context, bucket, key string) ([]byte, error) {
	v := a.store.GetMeta(bucket + "/" + key)
	return []byte(v), nil
}

// Set writes a value to the meta table.
func (a *kvAdapter) Set(_ context.Context, bucket, key string, value []byte) error {
	return a.store.SetMeta(bucket+"/"+key, string(value))
}

// Delete removes a key from the meta table.
func (a *kvAdapter) Delete(_ context.Context, bucket, key string) error {
	return a.store.DeleteMeta(bucket + "/" + key)
}

// List returns all keys in a bucket matching the prefix.
func (a *kvAdapter) List(_ context.Context, bucket, prefix string) ([]string, error) {
	allKeys := a.store.ListMeta()
	var out []string
	prefixFull := bucket + "/" + prefix
	for _, k := range allKeys {
		if !strings.HasPrefix(k, bucket+"/") {
			continue
		}
		suffix := k[len(bucket)+1:]
		if prefix == "" || strings.HasPrefix(suffix, prefix) {
			out = append(out, suffix)
		}
		_ = prefixFull // suppress unused
	}
	return out, nil
}
