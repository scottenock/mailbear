package store_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/store"
	"github.com/stretchr/testify/require"
)

func TestFileSaveAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s, err := store.NewFile(store.Options{Path: path})
	require.NoError(t, err)

	require.NoError(t, s.Save(domain.SubmissionRecord{Form: "contact", Email: "a@b.com", Delivered: true}))
	require.NoError(t, s.Save(domain.SubmissionRecord{Form: "contact", Email: "c@d.com", Delivered: false}))
	require.NoError(t, s.Close())

	records := readRecords(t, path)
	require.Len(t, records, 2)
	require.Equal(t, "a@b.com", records[0].Email)
	require.True(t, records[0].Delivered)
	require.Equal(t, "c@d.com", records[1].Email)
	require.False(t, records[1].Delivered)
}

func TestFileSaveConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s, err := store.NewFile(store.Options{Path: path})
	require.NoError(t, err)

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Save(domain.SubmissionRecord{Form: "contact", Subject: string(rune('a' + i%26))})
		}(i)
	}
	wg.Wait()
	require.NoError(t, s.Close())

	// Every concurrent write must produce exactly one intact, parseable line.
	require.Len(t, readRecords(t, path), n)
}

func TestFileRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	s, err := store.NewFile(store.Options{Path: path, MaxSizeMB: 1, MaxBackups: 3})
	require.NoError(t, err)

	// Write well over the 1 MB limit to force at least one rotation.
	big := strings.Repeat("x", 2048)
	for range 1500 {
		require.NoError(t, s.Save(domain.SubmissionRecord{Form: "contact", Content: big}))
	}
	require.NoError(t, s.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Greater(t, len(entries), 1, "rotation should produce at least one backup alongside the active log")
}

func readRecords(t *testing.T, path string) []domain.SubmissionRecord {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var records []domain.SubmissionRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec domain.SubmissionRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &rec))
		records = append(records, rec)
	}
	require.NoError(t, scanner.Err())
	return records
}
